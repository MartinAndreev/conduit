package bridge

import (
	"context"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"sync/atomic"
	"testing"
	"time"
)

type fakeInitializer struct {
	calls   atomic.Int32
	results chan startup.Outcome
	entered chan struct{}
}

func (f *fakeInitializer) Initialize(ctx context.Context, p *startup.Progress) startup.Outcome {
	f.calls.Add(1)
	f.entered <- struct{}{}
	return <-f.results
}
func newFake() *fakeInitializer {
	return &fakeInitializer{results: make(chan startup.Outcome, 4), entered: make(chan struct{}, 4)}
}
func eventual(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(time.Second)
	for !condition() {
		select {
		case <-deadline:
			t.Fatal("state did not settle")
		case <-time.After(time.Millisecond):
		}
	}
}
func terminal(id string, status startup.Status) startup.Outcome {
	return startup.Outcome{Snapshot: startup.Snapshot{OperationID: id, Revision: 4, Status: status, Steps: []startup.StepState{{ID: startup.CheckConfiguration, Status: startup.Completed}}}}
}
func TestReloadAndDoubleRetryKeepSingleOperation(t *testing.T) {
	f := newFake()
	b := New(f, nil, nil)
	b.Start()
	<-f.entered
	b.Start()
	if err := b.Retry(); err == nil {
		t.Fatal("retry accepted during run")
	}
	if f.calls.Load() != 1 {
		t.Fatal("duplicate start")
	}
	f.results <- terminal("one", startup.Blocked)
	eventual(t, func() bool { return b.Snapshot().CanRetry })
	if err := b.Retry(); err != nil {
		t.Fatal(err)
	}
	<-f.entered
	if err := b.Retry(); err == nil {
		t.Fatal("double retry accepted")
	}
	b.accept(1, startup.Snapshot{OperationID: "old", Revision: 999, Status: startup.Ready})
	if b.Snapshot().OperationID == "old" {
		t.Fatal("old generation replaced current")
	}
	b.accept(2, startup.Snapshot{OperationID: "two", Revision: 2, Status: startup.Running})
	b.accept(2, startup.Snapshot{OperationID: "two", Revision: 1, Status: startup.Ready})
	if b.Snapshot().Status != startup.Running {
		t.Fatal("old revision replaced current")
	}
	f.results <- terminal("two", startup.Cancelled)
	eventual(t, func() bool { return !b.Snapshot().Active })
	s := b.Snapshot()
	s.Steps[0].Status = startup.Failed
	if b.Snapshot().Steps[0].Status == startup.Failed {
		t.Fatal("snapshot aliases state")
	}
}

type fakeCleanup struct {
	complete atomic.Bool
	done     chan struct{}
}

func (f *fakeCleanup) Latest() startup.CleanupObservation {
	return startup.CleanupObservation{Complete: f.complete.Load()}
}
func (f *fakeCleanup) Done() <-chan struct{} { return f.done }
func (f *fakeCleanup) Await(ctx context.Context) (startup.CleanupObservation, error) {
	select {
	case <-ctx.Done():
		return f.Latest(), ctx.Err()
	case <-f.done:
		return f.Latest(), nil
	}
}
func TestPendingCleanupDisablesRetryAndAllowsExplicitExit(t *testing.T) {
	f := newFake()
	var exits atomic.Int32
	b := New(f, nil, func() { exits.Add(1) })
	b.Start()
	<-f.entered
	cleanup := &fakeCleanup{done: make(chan struct{})}
	o := terminal("one", startup.Cancelled)
	o.PendingCleanup = cleanup
	f.results <- o
	eventual(t, func() bool { return b.Snapshot().PendingCleanup })
	if err := b.Retry(); err == nil {
		t.Fatal("retry accepted with cleanup pending")
	}
	if b.await(10 * time.Millisecond) {
		t.Fatal("implicit exit accepted")
	}
	b.ExitNow()
	if exits.Load() != 1 {
		t.Fatal("explicit exit unavailable")
	}
	cleanup.complete.Store(true)
	close(cleanup.done)
	eventual(t, func() bool { return b.Snapshot().CanRetry })
	if !b.await(time.Second) {
		t.Fatal("completed cleanup not accepted")
	}
}
func TestBoundedCloseWhileInitializeStillActive(t *testing.T) {
	f := newFake()
	var exits atomic.Int32
	b := New(f, nil, func() { exits.Add(1) })
	b.Start()
	<-f.entered
	begin := time.Now()
	if b.await(10 * time.Millisecond) {
		t.Fatal("active run accepted implicit exit")
	}
	if time.Since(begin) > time.Second {
		t.Fatal("unbounded caller")
	}
	b.ExitNow()
	if exits.Load() != 1 {
		t.Fatal("explicit exit unavailable during active lookup")
	}
	f.results <- terminal("one", startup.Cancelled)
	eventual(t, func() bool { return !b.Snapshot().Active })
}

func TestCloseUncertaintyClearsAfterCompletion(t *testing.T) {
	f := newFake()
	b := New(f, nil, nil)
	b.Start()
	<-f.entered
	if !b.BeforeClose() {
		t.Fatal("active operation was not blocked")
	}
	if !b.Snapshot().Closing {
		t.Fatal("uncertainty not exposed")
	}
	f.results <- terminal("one", startup.Cancelled)
	eventual(t, func() bool { return !b.Snapshot().Active })
	state := b.Wait()
	if state.Closing || state.PendingCleanup || state.Active || !state.CanRetry {
		t.Fatalf("stale close state: %+v", state)
	}
}
