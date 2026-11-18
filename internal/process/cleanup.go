package process

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/MartinAndreev/conduit/internal/lockscope"
)

// CleanupState separates reaping the root from the result of requesting tree
// termination. A successful signal is not proof that the OS is quiescent.
// ClosureState distinguishes a pending Close from completion with failure.
type ClosureState struct {
	Complete bool
	Err      error
}
type CleanupState struct {
	StdinClosure  ClosureState
	StdoutClosure ClosureState
	StderrClosure ClosureState

	TreeTerminationRequested bool
	TreeTerminationErr       error
	PipesClosed              bool
	PipeCloseErr             error
	StdinCloseErr            error
	ReadersDone              bool
	RootReaped               bool
	RootResult               Result
	Complete                 bool
}

// CleanupOwner retains responsibility for one process after a caller stops
// waiting. BeginCleanup performs one cleanup attempt, without retrying: tree
// kill and, right after it, the registration of finalization (which ends
// signalling and may reap the root before the pipes close), then pipe close,
// Wait and finalization's terminal outcome.
type CleanupOwner struct {
	child       Process
	mu          sync.Mutex
	state       CleanupState
	begin       sync.Once
	rootDone    chan struct{}
	reapDone    chan struct{}
	done        chan struct{}
	stdinDone   chan struct{}
	drainDone   <-chan struct{}
	readersDone chan struct{}
	// priorKill, when set, is the outcome of an earlier group kill by the
	// child's previous owner, used when finalization refuses BeginCleanup's.
	priorKill *error
}

// CleanupOption configures a CleanupOwner.
type CleanupOption func(*CleanupOwner)

// WithPriorKill hands the owner the outcome of a group kill that an earlier
// owner of the child already sent (for example a loss path that then ended
// signalling). If finalization refuses BeginCleanup's own kill, that outcome
// is the termination evidence; without it the refusal stays unconfirmed.
func WithPriorKill(err error) CleanupOption {
	return func(o *CleanupOwner) { o.priorKill = &err }
}

func NewCleanupOwner(child Process) *CleanupOwner {
	done := make(chan struct{})
	close(done)
	return NewCleanupOwnerWithDrain(child, done)
}

// NewCleanupOwnerWithDrain includes the caller's capture drain workers in
// completion. Closing stdin is asynchronous, so even a slow Close stays owned.
func NewCleanupOwnerWithDrain(child Process, drainDone <-chan struct{}, options ...CleanupOption) *CleanupOwner {
	o := &CleanupOwner{child: child, rootDone: make(chan struct{}), reapDone: make(chan struct{}), done: make(chan struct{}), stdinDone: make(chan struct{}), drainDone: drainDone, readersDone: make(chan struct{})}
	for _, option := range options {
		option(o)
	}
	go func() {
		err := closeCapturePipe(child.Stdin())
		_ = lockscope.WithMutexLock(&o.mu, func() error {
			o.state.StdinCloseErr = err
			o.state.StdinClosure = ClosureState{Complete: true, Err: err}
			return nil
		})
		close(o.stdinDone)
	}()
	go func() {
		<-o.drainDone
		_ = lockscope.WithMutexLock(&o.mu, func() error { o.state.ReadersDone = true; return nil })
		close(o.readersDone)
	}()
	go o.awaitRoot()
	return o
}

// awaitRoot records Wait's result. A child that implements Finalizer keeps
// its root unreaped until BeginCleanup finalizes it; any other child counts
// as reaped once Wait returned.
func (o *CleanupOwner) awaitRoot() {
	result := o.child.Wait()
	_, finalizer := o.child.(Finalizer)
	// A child without Finalizer counts as reaped by Wait, published in the
	// same scope as its result, before RootDone closes.
	_ = lockscope.WithMutexLock(&o.mu, func() error {
		o.state.RootResult = result
		o.state.RootReaped = !finalizer
		return nil
	})
	close(o.rootDone)
	if !finalizer {
		close(o.reapDone)
	}
}

// finalize records finalization's terminal outcome once Wait returned (the
// root exited, so it is reached without blocking on the root). A failed
// release (failed reap or ErrReleaseUnavailable) is reported without
// RootReaped: its error joins RootResult.ReleaseErr and cleanup completes
// with it.
func (o *CleanupOwner) finalize() {
	<-o.rootDone
	if finalizer, ok := o.child.(Finalizer); ok {
		o.reaped(finalizer.Finalize(context.Background()))
	}
}

func (o *CleanupOwner) reaped(err error) {
	_ = lockscope.WithMutexLock(&o.mu, func() error {
		o.state.RootResult.ReleaseErr = errors.Join(o.state.RootResult.ReleaseErr, err)
		o.state.RootReaped = err == nil
		return nil
	})
	close(o.reapDone)
}

// RootDone closes once Wait returned: the root exited and its pipes ended.
// A Finalizer child's root is still unreaped then, so KillTree in
// BeginCleanup still reaches its group; Latest reports RootReaped, and Done
// closes, only after BeginCleanup finalized it. Every owner must end with
// BeginCleanup, also after a clean exit.
func (o *CleanupOwner) RootDone() <-chan struct{} { return o.rootDone }
func (o *CleanupOwner) Done() <-chan struct{}     { return o.done }
func (o *CleanupOwner) Latest() (state CleanupState) {
	_ = lockscope.WithMutexLock(&o.mu, func() error { state = o.state; return nil })
	return
}
func (o *CleanupOwner) BeginCleanup() {
	o.begin.Do(func() {
		_ = lockscope.WithMutexLock(&o.mu, func() error { o.state.TreeTerminationRequested = true; return nil })
		go func() {
			killErr := o.child.KillTree()
			// The kill is the owner's last signal: finalization is registered
			// now, independently of the pipes; finalize waits for its outcome.
			RequestFinalize(o.child)
			killErr = o.terminationOutcome(killErr)
			_ = lockscope.WithMutexLock(&o.mu, func() error { o.state.TreeTerminationErr = killErr; return nil })
			stdoutErr := closeCapturePipe(o.child.Stdout())
			_ = lockscope.WithMutexLock(&o.mu, func() error {
				o.state.StdoutClosure = ClosureState{Complete: true, Err: stdoutErr}
				o.state.PipeCloseErr = stdoutErr
				return nil
			})
			stderrErr := closeCapturePipe(o.child.Stderr())
			_ = lockscope.WithMutexLock(&o.mu, func() error {
				o.state.StderrClosure = ClosureState{Complete: true, Err: stderrErr}
				o.state.PipeCloseErr = errors.Join(stdoutErr, stderrErr)
				o.state.PipesClosed = o.state.PipeCloseErr == nil
				return nil
			})
			o.finalize()
			<-o.reapDone
			<-o.stdinDone
			<-o.readersDone
			_ = lockscope.WithMutexLock(&o.mu, func() error {
				o.state.Complete = true
				return nil
			})
			close(o.done)
		}()
	})
}

// terminationOutcome is TreeTerminationErr for BeginCleanup's kill: nil when
// it confirmed termination (KillConfirmed). A refusal by finalization sent
// nothing: the prior kill's outcome stands when there was one, otherwise the
// refusal itself is kept, so termination stays unconfirmed.
func (o *CleanupOwner) terminationOutcome(killErr error) error {
	if errors.Is(killErr, ErrSignalsFinalized) && o.priorKill != nil {
		killErr = *o.priorKill
	}
	if KillConfirmed(killErr) {
		return nil
	}
	return killErr
}

func (o *CleanupOwner) Await(ctx context.Context) (CleanupState, error) {
	select {
	case <-o.done:
		return o.Latest(), nil
	default:
	}
	select {
	case <-o.done:
		return o.Latest(), nil
	case <-ctx.Done():
		return o.Latest(), ctx.Err()
	}
}

// os.ErrClosed confirms a pipe was already released by its drain worker.
func closeCapturePipe(pipe io.Closer) error {
	err := pipe.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
