package startup

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/MartinAndreev/conduit/internal/configuration"
)

type fakeRepository struct {
	reads        []ReadResult
	events       *[]string
	publishes    int
	publishKind  PublishKind
	publishErr   error
	afterPublish func()
	proposed     configuration.Config
}

func (r *fakeRepository) Resolve(context.Context) (string, error) {
	*r.events = append(*r.events, "resolve")
	return filepath.Join("/", "home", ".conduit", "config.yaml"), nil
}
func (r *fakeRepository) Read(context.Context, string) ReadResult {
	*r.events = append(*r.events, "read")
	next := r.reads[0]
	if len(r.reads) > 1 {
		r.reads = r.reads[1:]
	}
	return next
}
func (r *fakeRepository) Publish(_ context.Context, _ string, c configuration.Config) (PublishKind, error) {
	*r.events = append(*r.events, "publish")
	r.publishes++
	r.proposed = c
	if r.afterPublish != nil {
		r.afterPublish()
	}
	if r.publishKind == "" && r.publishErr == nil {
		r.publishKind = Published
	}
	return r.publishKind, r.publishErr
}

type fakeDetector struct {
	events  *[]string
	kind    configuration.DiscoveryKind
	version string
	cancel  context.CancelFunc
	calls   int
	err     error
}

func (d *fakeDetector) Discover(_ context.Context, h configuration.HarnessID, override string) (configuration.Observation, error) {
	*d.events = append(*d.events, string(h)+":"+override)
	d.calls++
	if d.cancel != nil {
		d.cancel()
	}
	return configuration.Observation{Kind: d.kind, Path: func() string {
		if d.kind == configuration.Missing {
			return ""
		}
		return filepath.Join("/", "tools", string(h))
	}(), Version: d.version}, d.err
}
func setup(t *testing.T) (*fakeRepository, *fakeDetector, *[]string) {
	t.Helper()
	events := []string{}
	r := &fakeRepository{events: &events, reads: []ReadResult{{Kind: Absent}, {Kind: Valid}}}
	d := &fakeDetector{events: &events, kind: configuration.Found, version: "1.2.3"}
	return r, d, &events
}
func TestFirstRunOrder(t *testing.T) {
	r, d, events := setup(t)
	p := NewProgress()
	result := (Service{r, d}).Initialize(context.Background(), p)
	if result.Snapshot.Status != Ready || !result.Published {
		t.Fatal(result)
	}
	if !reflect.DeepEqual(*events, []string{"resolve", "read", "codex:", "opencode:", "publish", "read"}) {
		t.Fatal(*events)
	}
	if len(r.proposed.Assignments) != 0 {
		t.Fatal(r.proposed)
	}
	last, ok := p.Latest()
	if !ok || last.Revision != result.Snapshot.Revision {
		t.Fatal(last)
	}
}
func TestNoHarnessWarning(t *testing.T) {
	r, d, _ := setup(t)
	d.kind = configuration.Missing
	d.version = ""
	result := (Service{r, d}).Initialize(context.Background(), nil)
	if result.Snapshot.Status != ReadyWithWarnings || len(result.Snapshot.Diagnostics) != 2 {
		t.Fatal(result)
	}
}
func TestExistingConfigUsesOverridesWithoutWrite(t *testing.T) {
	r, d, _ := setup(t)
	r.reads = []ReadResult{{Kind: Valid, Config: configuration.Config{Harnesses: map[configuration.HarnessID]configuration.Harness{configuration.Codex: {Executable: "/explicit"}}}}}
	out := (Service{r, d}).Initialize(context.Background(), nil)
	if out.Snapshot.Status != Ready || r.publishes != 0 {
		t.Fatal(out)
	}
	if (*r.events)[2] != "codex:/explicit" {
		t.Fatal(*r.events)
	}
}
func TestBadInitialConfigStopsEffects(t *testing.T) {
	for _, kind := range []ReadKind{Invalid, Unreadable, FutureSchema} {
		t.Run(string(kind), func(t *testing.T) {
			r, d, _ := setup(t)
			r.reads = []ReadResult{{Kind: kind, Err: errors.New("unsafe-secret")}}
			out := (Service{r, d}).Initialize(context.Background(), nil)
			if out.Snapshot.Status != Blocked || d.calls != 0 || r.publishes != 0 {
				t.Fatal(out)
			}
			if out.Snapshot.Diagnostics[0].Details == "unsafe-secret" {
				t.Fatal("leaked error")
			}
		})
	}
}
func TestCancellationBeforeDuringAndAfterPublish(t *testing.T) {
	for _, point := range []string{"before", "probe", "publish"} {
		t.Run(point, func(t *testing.T) {
			r, d, _ := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch point {
			case "before":
				cancel()
			case "probe":
				d.cancel = cancel
			case "publish":
				r.afterPublish = cancel
			}
			out := (Service{r, d}).Initialize(ctx, nil)
			if out.Snapshot.Status != Cancelled || !errors.Is(out.Err, context.Canceled) {
				t.Fatal(out)
			}
			if point == "publish" && !out.Published {
				t.Fatal("publication lost")
			}
			if point != "publish" && r.publishes != 0 {
				t.Fatal("published after cancellation")
			}
			if point == "probe" && d.calls != 1 {
				t.Fatal(d.calls)
			}
		})
	}
}
func TestPublicationReconciliationAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       PublishKind
		publishErr error
		final      ReadKind
		published  bool
	}{{"winner", AlreadyExists, nil, Valid, false}, {"invalid winner", AlreadyExists, nil, Invalid, false}, {"failed reread", Published, nil, Unreadable, true}, {"write failure", "", errors.New("disk full"), Valid, false}, {"post publish sync failure", Published, errors.New("sync failed"), Valid, true}} {
		t.Run(tc.name, func(t *testing.T) {
			r, d, _ := setup(t)
			r.publishKind = tc.kind
			r.publishErr = tc.publishErr
			r.reads[1] = ReadResult{Kind: tc.final, Err: errors.New("read failed")}
			out := (Service{r, d}).Initialize(context.Background(), nil)
			want := Blocked
			if tc.kind == AlreadyExists && tc.final == Valid {
				want = Ready
			}
			if out.Snapshot.Status != want || out.Published != tc.published {
				t.Fatal(out)
			}
		})
	}
}
func TestRetryCreatesNewOperationAndRereads(t *testing.T) {
	r, d, _ := setup(t)
	r.reads = []ReadResult{{Kind: Invalid}, {Kind: Valid}}
	s := Service{r, d}
	first := s.Initialize(context.Background(), nil)
	second := s.Initialize(context.Background(), nil)
	if first.Snapshot.Status != Blocked || second.Snapshot.Status != Ready || first.Snapshot.OperationID == second.Snapshot.OperationID {
		t.Fatal(first, second)
	}
}
func TestMailboxCopiesAndDoesNotBlock(t *testing.T) {
	p := NewProgress()
	for i := 0; i < 10000; i++ {
		p.publish(Snapshot{Revision: uint64(i), Steps: []StepState{{Status: Completed}}})
	}
	snapshot, ok := p.Latest()
	if !ok || snapshot.Revision != 9999 {
		t.Fatal(snapshot)
	}
	snapshot.Steps[0].Status = Failed
	again, _ := p.Latest()
	if again.Steps[0].Status != Completed {
		t.Fatal("mutable snapshot")
	}
	p.Close()
	p.publish(Snapshot{})
	p.Close()
}
func TestMailboxConcurrentCloseReadPublish(t *testing.T) {
	p := NewProgress()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p.publish(Snapshot{Revision: uint64(i)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p.Latest()
		}
	}()
	go func() { defer wg.Done(); p.Close() }()
	wg.Wait()
}

func TestClosedMailboxDoesNotLoseTerminalOutcome(t *testing.T) {
	r, d, _ := setup(t)
	p := NewProgress()
	p.Close()
	out := (Service{r, d}).Initialize(context.Background(), p)
	if out.Snapshot.Status != Ready {
		t.Fatal(out)
	}
}
func TestExplicitMissingHasSpecificDiagnostic(t *testing.T) {
	r, d, _ := setup(t)
	r.reads = []ReadResult{{Kind: Valid, Config: configuration.Config{Harnesses: map[configuration.HarnessID]configuration.Harness{configuration.Codex: {Executable: "/explicit"}}}}}
	d.kind = configuration.Missing
	d.version = ""
	out := (Service{r, d}).Initialize(context.Background(), nil)
	if out.Snapshot.Diagnostics[0].Code != "harness_override_missing" {
		t.Fatal(out)
	}
}

func TestProbeCleanupFailureStopsStartupAndPreservesCause(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked", true: "cancelled"}[cancelled], func(t *testing.T) {
			repository, detector, _ := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("unsafe technical cause")
			detector.err = errors.Join(ErrProbeCleanup, cause)
			if cancelled {
				detector.cancel = cancel
			}
			outcome := (Service{repository, detector}).Initialize(ctx, nil)
			want := Blocked
			if cancelled {
				want = Cancelled
			}
			if outcome.Snapshot.Status != want || repository.publishes != 0 || detector.calls != 1 {
				t.Fatal(outcome, repository.publishes, detector.calls)
			}
			if !errors.Is(outcome.Err, ErrProbeCleanup) || !errors.Is(outcome.Err, cause) {
				t.Fatal("cleanup cause lost", outcome.Err)
			}
			if cancelled && !errors.Is(outcome.Err, context.Canceled) {
				t.Fatal(outcome.Err)
			}
			diagnostics := outcome.Snapshot.Diagnostics
			if len(diagnostics) != 1 || diagnostics[0].Code != "harness_probe_cleanup_unconfirmed" || diagnostics[0].Severity != Error || diagnostics[0].Harness != configuration.Codex {
				t.Fatal(diagnostics)
			}
			if diagnostics[0].Details == cause.Error() {
				t.Fatal("technical cause leaked")
			}
		})
	}
}

type testCleanupObserver struct{ done chan struct{} }

func (o *testCleanupObserver) Latest() CleanupObservation { return CleanupObservation{} }
func (o *testCleanupObserver) Done() <-chan struct{}      { return o.done }
func (o *testCleanupObserver) Await(ctx context.Context) (CleanupObservation, error) {
	return o.Latest(), ctx.Err()
}

type testPendingCleanup struct {
	observer CleanupObserver
	cause    error
}

func (e testPendingCleanup) Error() string                    { return "cleanup pending" }
func (e testPendingCleanup) Unwrap() error                    { return errors.Join(ErrProbeCleanup, e.cause) }
func (e testPendingCleanup) CleanupObserver() CleanupObserver { return e.observer }
func TestOutcomeRetainsPendingCleanupObserver(t *testing.T) {
	repository, detector, _ := setup(t)
	observer := &testCleanupObserver{done: make(chan struct{})}
	detector.err = testPendingCleanup{observer, context.DeadlineExceeded}
	outcome := (Service{repository, detector}).Initialize(context.Background(), nil)
	if outcome.PendingCleanup != observer || repository.publishes != 0 || outcome.Snapshot.Status != Blocked {
		t.Fatal(outcome)
	}
}

func TestCancelledDiscoveryPreservesLaunchCause(t *testing.T) {
	repository, detector, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("launch failed")
	detector.cancel = cancel
	detector.err = errors.Join(context.Canceled, cause)
	outcome := (Service{repository, detector}).Initialize(ctx, nil)
	if outcome.Snapshot.Status != Cancelled || !errors.Is(outcome.Err, context.Canceled) || !errors.Is(outcome.Err, cause) || repository.publishes != 0 {
		t.Fatal(outcome)
	}
}
func TestCancelledPublishedConfigurationPreservesSyncCause(t *testing.T) {
	repository, detector, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("directory sync failed after publish")
	repository.publishKind = Published
	repository.publishErr = cause
	repository.afterPublish = cancel
	outcome := (Service{repository, detector}).Initialize(ctx, nil)
	if outcome.Snapshot.Status != Cancelled || !outcome.Published || !errors.Is(outcome.Err, context.Canceled) || !errors.Is(outcome.Err, cause) {
		t.Fatal(outcome)
	}
}
