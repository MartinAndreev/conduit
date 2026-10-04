package bridge

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"time"
)

var ErrUnavailable = errors.New("startup is active or cleanup remains unconfirmed")

func New(initializer Initializer, emit func(State), quit func()) *Bridge {
	return &Bridge{initializer: initializer, context: context.Background(), emit: emit, quit: quit}
}
func (b *Bridge) SetContext(ctx context.Context) {
	_ = lockscope.WithMutexLock(&b.mu, func() error { b.context = ctx; return nil })
}
func copyState(s State) State {
	s.Steps = append([]startup.StepState(nil), s.Steps...)
	s.Observations = append([]startup.HarnessObservation(nil), s.Observations...)
	s.Diagnostics = append([]startup.Diagnostic(nil), s.Diagnostics...)
	return s
}
func (b *Bridge) Snapshot() State {
	var state State
	_ = lockscope.WithMutexLock(&b.mu, func() error { state = copyState(b.state); return nil })
	return state
}

// Start is idempotent for reload: it starts only the first operation.
func (b *Bridge) Start() State {
	_ = b.begin(false)
	return b.Snapshot()
}
func (b *Bridge) Retry() error { return b.begin(true) }
func (b *Bridge) begin(retry bool) error {
	var ctx context.Context
	var progress *startup.Progress
	var generation uint64
	err := lockscope.WithMutexLock(&b.mu, func() error {
		if b.state.Active || b.state.PendingCleanup {
			return ErrUnavailable
		}
		if !retry && b.generation != 0 {
			return nil
		}
		if retry && !b.state.CanRetry {
			return ErrUnavailable
		}
		b.generation++
		generation = b.generation
		ctx, b.cancel = context.WithCancel(b.context)
		b.done = make(chan struct{})
		b.outcome = startup.Outcome{}
		b.state = State{Generation: generation, Version: 1, Active: true, Status: startup.Running}
		progress = startup.NewProgress()
		return nil
	})
	if err != nil || progress == nil {
		return err
	}
	b.notify()
	go b.run(ctx, progress, generation)
	return nil
}
func (b *Bridge) run(ctx context.Context, progress *startup.Progress, generation uint64) {
	result := make(chan startup.Outcome, 1)
	go func() { result <- b.initializer.Initialize(ctx, progress) }()
	for {
		select {
		case <-progress.Changed():
			if snapshot, ok := progress.Latest(); ok {
				b.accept(generation, snapshot)
			}
		case outcome := <-result:
			progress.Close()
			b.finish(generation, outcome)
			return
		}
	}
}
func (b *Bridge) accept(generation uint64, s startup.Snapshot) {
	changed := false
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		if generation != b.generation || !b.state.Active || s.Revision <= b.state.Revision {
			return nil
		}
		b.state.Version++
		b.state.Revision = s.Revision
		b.state.OperationID = s.OperationID
		b.state.Status = s.Status
		b.state.Steps = append([]startup.StepState(nil), s.Steps...)
		b.state.Observations = append([]startup.HarnessObservation(nil), s.Observations...)
		b.state.Diagnostics = append([]startup.Diagnostic(nil), s.Diagnostics...)
		b.state.ConfigPath = s.ConfigPath
		changed = true
		return nil
	})
	if changed {
		b.notify()
	}
}
func (b *Bridge) finish(generation uint64, outcome startup.Outcome) {
	b.accept(generation, outcome.Snapshot)
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		if generation != b.generation {
			return nil
		}
		b.outcome = outcome
		b.state.Version++
		b.state.Active = false
		b.state.PendingCleanup = outcome.PendingCleanup != nil && !outcome.PendingCleanup.Latest().Complete
		b.state.CanRetry = !b.state.PendingCleanup && (b.state.Status == startup.Blocked || b.state.Status == startup.Cancelled)
		b.state.Closing = b.state.Closing && b.state.PendingCleanup
		close(b.done)
		return nil
	})
	b.notify()
	if outcome.PendingCleanup != nil {
		go b.observeCleanup(generation, outcome.PendingCleanup)
	}
}
func (b *Bridge) observeCleanup(generation uint64, observer startup.CleanupObserver) {
	<-observer.Done()
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		if generation != b.generation {
			return nil
		}
		b.state.Version++
		b.state.PendingCleanup = !observer.Latest().Complete
		b.state.Closing = b.state.Closing && (b.state.Active || b.state.PendingCleanup)
		b.state.CanRetry = !b.state.PendingCleanup && (b.state.Status == startup.Blocked || b.state.Status == startup.Cancelled)
		return nil
	})
	b.notify()
}
func (b *Bridge) notify() {
	if b.emit != nil {
		b.emit(b.Snapshot())
	}
}
func (b *Bridge) Cancel() {
	var cancel context.CancelFunc
	_ = lockscope.WithMutexLock(&b.mu, func() error { cancel = b.cancel; return nil })
	if cancel != nil {
		cancel()
	}
}

// Wait bounds only the caller; the observer remains owned by this bridge.
func (b *Bridge) Wait() State { b.await(2 * time.Second); return b.Snapshot() }
func (b *Bridge) await(limit time.Duration) bool {
	b.Cancel()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	var done chan struct{}
	_ = lockscope.WithMutexLock(&b.mu, func() error { done = b.done; return nil })
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return false
		}
	}
	var observer startup.CleanupObserver
	var generation uint64
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		observer = b.outcome.PendingCleanup
		generation = b.generation
		return nil
	})
	if observer != nil {
		_, _ = observer.Await(ctx)
		_ = lockscope.WithMutexLock(&b.mu, func() error {
			if b.generation == generation {
				b.state.PendingCleanup = !observer.Latest().Complete
				b.state.Closing = b.state.Closing && (b.state.Active || b.state.PendingCleanup)
				b.state.CanRetry = !b.state.Active && !b.state.PendingCleanup && (b.state.Status == startup.Blocked || b.state.Status == startup.Cancelled)
				b.state.Version++
			}
			return nil
		})
		b.notify()
	}
	state := b.Snapshot()
	return !state.Active && !state.PendingCleanup
}

// BeforeClose prevents an implicit exit while cleanup is uncertain.
func (b *Bridge) BeforeClose() bool {
	b.await(2 * time.Second)
	var blocked bool
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		blocked = b.state.Active || b.state.PendingCleanup
		b.state.Closing = blocked
		b.state.Version++
		return nil
	})
	b.notify()
	return blocked
}
func (b *Bridge) Exit() {
	if !b.BeforeClose() && b.quit != nil {
		b.quit()
	}
}

// ExitNow is available only from the explicit uncertainty action.
func (b *Bridge) ExitNow() {
	if b.quit != nil {
		b.quit()
	}
}
