package process

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
)

// scriptedRoot replaces the root's OS effects so each test controls the order
// of exit observation, reap and signals.
type scriptedRoot struct {
	exit        chan scriptedExit
	reapErr     error
	reapStarted chan struct{}
	reapGate    *gate
	killStarted chan struct{}
	killGate    *gate
	// waitable, when set, scripts each waitability query in order; a send
	// completes only when the worker asks. Unset, the root is waitable.
	waitable chan waitableAnswer
	// hooks receives every lockHook point.
	hooks chan string
	// stop releases every scripted wait when the test ends, also after a
	// failed assertion.
	stop chan struct{}

	mu      sync.Mutex
	reaps   int
	signals []string
}

type waitableAnswer struct {
	waitable bool
	err      error
}

// scriptedExit is the observed exit; held false means the platform reaped
// the root at exit.
type scriptedExit struct {
	result Result
	held   bool
}

// gate blocks a scripted effect until the test opens it.
type gate struct {
	ch   chan struct{}
	once sync.Once
}

func newGate(open bool) *gate {
	g := &gate{ch: make(chan struct{})}
	if open {
		g.open()
	}
	return g
}

func (g *gate) open() { g.once.Do(func() { close(g.ch) }) }

func newScriptedRoot() *scriptedRoot {
	return &scriptedRoot{
		exit:        make(chan scriptedExit, 1),
		reapStarted: make(chan struct{}, 1),
		reapGate:    newGate(true),
		killStarted: make(chan struct{}, 1),
		killGate:    newGate(true),
		hooks:       make(chan string, 16),
		stop:        make(chan struct{}),
	}
}

func (r *scriptedRoot) ops(signalsByNumber bool) rootOps {
	return rootOps{
		awaitExit: func() (Result, bool) {
			select {
			case exit := <-r.exit:
				return exit.result, exit.held
			case <-r.stop:
				return Result{}, true
			}
		},
		waitable: func() (bool, error) {
			if r.waitable == nil {
				return true, nil
			}
			select {
			case answer := <-r.waitable:
				return answer.waitable, answer.err
			case <-r.stop:
				return false, errors.New("test ended")
			}
		},
		lockHook: func(point string) {
			select {
			case r.hooks <- point:
			default:
			}
		},
		reap: func() error {
			_ = lockscope.WithMutexLock(&r.mu, func() error {
				r.reaps++
				return nil
			})
			notify(r.reapStarted)
			select {
			case <-r.reapGate.ch:
			case <-r.stop:
			}
			return r.reapErr
		},
		interrupt: func() error { r.record("interrupt"); return nil },
		killTree: func() error {
			r.record("kill")
			notify(r.killStarted)
			select {
			case <-r.killGate.ch:
			case <-r.stop:
			}
			return nil
		},
		signalsByNumber: signalsByNumber,
	}
}

func notify(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

func (r *scriptedRoot) record(signal string) {
	_ = lockscope.WithMutexLock(&r.mu, func() error {
		r.signals = append(r.signals, signal)
		return nil
	})
}

func (r *scriptedRoot) sent() (signals []string) {
	_ = lockscope.WithMutexLock(&r.mu, func() error {
		signals = slices.Clone(r.signals)
		return nil
	})
	return signals
}

func (r *scriptedRoot) reapCount() (reaps int) {
	_ = lockscope.WithMutexLock(&r.mu, func() error {
		reaps = r.reaps
		return nil
	})
	return reaps
}

// startScripted runs the supervisor over idle pipes; the test ends them by
// closing the process's stdout and stderr. Cleanup, also after a failed
// assertion, releases every scripted wait and gate, closes the tracked pipes
// and joins the supervisor within a bound.
func startScripted(t *testing.T, root *scriptedRoot, signalsByNumber bool) *execProcess {
	t.Helper()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	stdinReader, stdinWriter := io.Pipe()
	proc := newExecProcess(42, root.ops(signalsByNumber), &ownedResources{}, stdinWriter, stdoutReader, stderrReader)
	t.Cleanup(func() {
		close(root.stop)
		root.reapGate.open()
		root.killGate.open()
		endPipes(proc)
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		_ = stdinReader.Close()
		select {
		case <-proc.supervised:
		case <-time.After(5 * time.Second):
			t.Error("scripted supervisor did not finish")
		}
	})
	go proc.superviseRoot()
	return proc
}

func endPipes(proc *execProcess) {
	_ = proc.Stdout().Close()
	_ = proc.Stderr().Close()
}

func awaitSignal(t *testing.T, channel <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not happen", what)
	}
}

// awaitHook waits until a goroutine found signalMu held at point (its
// TryLock failed) and goes on to acquire it.
func awaitHook(t *testing.T, root *scriptedRoot, point string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-root.hooks:
			if got == point {
				return
			}
		case <-deadline:
			t.Fatalf("nothing reached signalMu at %q", point)
		}
	}
}

// answerWaitable replies to the worker's next waitability query.
func answerWaitable(t *testing.T, root *scriptedRoot, answer waitableAnswer) {
	t.Helper()
	select {
	case root.waitable <- answer:
	case <-time.After(5 * time.Second):
		t.Fatal("no waitability query")
	}
}

// countScripted makes proc count in UnfinalizedProcesses like a launched
// process, and removes what the test leaves counted.
func countScripted(t *testing.T, proc *execProcess) {
	t.Helper()
	proc.counted = true
	unfinalized.Add(1)
	t.Cleanup(func() {
		select {
		case <-proc.finalDone:
			if proc.finalErr == nil {
				return
			}
		default:
		}
		unfinalized.Add(-1)
	})
}

func waitResult(t *testing.T, proc *execProcess) Result {
	t.Helper()
	done := make(chan Result, 1)
	go func() { done <- proc.Wait() }()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return")
		return Result{}
	}
}

func finalize(t *testing.T, proc *execProcess) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return proc.Finalize(ctx)
}

func assertProcessDone(t *testing.T, proc *execProcess) {
	t.Helper()
	if err := proc.KillTree(); !errors.Is(err, ErrSignalsFinalized) || !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("KillTree after Finalize = %v, want os.ErrProcessDone", err)
	}
	if err := proc.Interrupt(); !errors.Is(err, ErrSignalsFinalized) {
		t.Fatalf("Interrupt after Finalize = %v, want ErrSignalsFinalized", err)
	}
}

// The root stays unreaped after its exit and after Wait, so signals sent
// before the exit, after it and after Wait (a descendant with redirected
// stdio) are delivered. Only Finalize reaps; nothing is sent afterwards.
func TestSignalsReachRootUntilFinalize(t *testing.T) {
	root := newScriptedRoot()
	proc := startScripted(t, root, true)
	if err := proc.Interrupt(); err != nil {
		t.Fatalf("Interrupt before exit: %v", err)
	}
	root.exit <- scriptedExit{result: Result{ExitCode: 1}, held: true}
	awaitSignal(t, proc.RootExited(), "root exit")
	if err := proc.Interrupt(); err != nil {
		t.Fatalf("Interrupt after exit: %v", err)
	}
	endPipes(proc)
	if result := waitResult(t, proc); result.ExitCode != 1 {
		t.Fatalf("Wait = %+v, want exit 1", result)
	}
	if err := proc.KillTree(); err != nil {
		t.Fatalf("KillTree after Wait: %v", err)
	}
	if reaps := root.reapCount(); reaps != 0 {
		t.Fatalf("root reaped %d times before Finalize", reaps)
	}
	if err := finalize(t, proc); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	assertProcessDone(t, proc)
	if got, want := root.sent(), []string{"interrupt", "interrupt", "kill"}; !slices.Equal(got, want) {
		t.Fatalf("signals sent = %v, want %v", got, want)
	}
	if reaps := root.reapCount(); reaps != 1 {
		t.Fatalf("root reaped %d times, want once", reaps)
	}
}

// Finalize registers the intent before the exit: its context can end while
// the root still runs, and a KillTree then still reaches it. The worker owns
// the release from then on: after the exit it commits and reaps without
// another call.
func TestFinalizeBeforeExitKeepsSignallingUntilExit(t *testing.T) {
	root := newScriptedRoot()
	proc := startScripted(t, root, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := proc.Finalize(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Finalize before exit = %v, want context.Canceled", err)
	}
	if err := proc.KillTree(); err != nil {
		t.Fatalf("KillTree after the intent, before exit: %v", err)
	}
	root.exit <- scriptedExit{held: true}
	awaitSignal(t, root.reapStarted, "reap by the worker after the exit")
	awaitSignal(t, proc.finalDone, "finalization outcome")
	assertProcessDone(t, proc)
	if got := root.sent(); !slices.Equal(got, []string{"kill"}) {
		t.Fatalf("signals sent = %v, want the kill only (Finalize never kills)", got)
	}
	if err := finalize(t, proc); err != nil {
		t.Fatalf("Finalize after the reap: %v", err)
	}
}

func TestFinalizeIsIdempotentAndConcurrent(t *testing.T) {
	root := newScriptedRoot()
	root.reapErr = errors.New("reap failed")
	proc := startScripted(t, root, true)
	countScripted(t, proc)
	before := UnfinalizedProcesses()
	root.exit <- scriptedExit{held: true}
	results := make(chan error, 8)
	for range 8 {
		go func() { results <- finalize(t, proc) }()
	}
	for range 8 {
		if err := <-results; err != root.reapErr {
			t.Fatalf("Finalize = %v, want the single reap's %v", err, root.reapErr)
		}
	}
	if reaps := root.reapCount(); reaps != 1 {
		t.Fatalf("root reaped %d times, want once", reaps)
	}
	// A failed reap keeps signals refused and the process counted.
	assertProcessDone(t, proc)
	if after := UnfinalizedProcesses(); after != before {
		t.Fatalf("failed reap changed the unfinalized count %d -> %d", before, after)
	}
}

// After the commit a KillTree is refused at once, also while the reap runs.
func TestKillTreeAfterCommitIsRefused(t *testing.T) {
	root := newScriptedRoot()
	root.reapGate = newGate(false)
	proc := startScripted(t, root, true)
	root.exit <- scriptedExit{held: true}
	finalized := make(chan error, 1)
	go func() { finalized <- finalize(t, proc) }()
	awaitSignal(t, root.reapStarted, "reap")
	if err := proc.KillTree(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("KillTree during the reap = %v, want os.ErrProcessDone", err)
	}
	root.reapGate.open()
	if err := <-finalized; err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sent := root.sent(); len(sent) != 0 {
		t.Fatalf("signals sent after the commit: %v", sent)
	}
}

// A KillTree in flight (holding signalMu) holds the commit, and so the reap,
// off until it returned. The worker is known to wait on signalMu.
func TestFinalizeWaitsForKillInFlight(t *testing.T) {
	root := newScriptedRoot()
	root.killGate = newGate(false)
	proc := startScripted(t, root, true)
	root.exit <- scriptedExit{held: true}
	awaitSignal(t, proc.RootExited(), "root exit")
	killed := make(chan error, 1)
	go func() { killed <- proc.KillTree() }()
	awaitSignal(t, root.killStarted, "kill")
	finalized := make(chan error, 1)
	go func() { finalized <- finalize(t, proc) }()
	awaitHook(t, root, hookCommit)
	if proc.signalMu.TryLock() {
		t.Fatal("signalMu is free while a kill is in flight")
	}
	if reaps := root.reapCount(); reaps != 0 {
		t.Fatalf("root reaped %d times while a kill holds signalMu", reaps)
	}
	root.killGate.open()
	if err := <-killed; err != nil {
		t.Fatalf("KillTree: %v", err)
	}
	if err := <-finalized; err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if reaps := root.reapCount(); reaps != 1 {
		t.Fatalf("root reaped %d times, want once", reaps)
	}
}

// A transient negative waitability answer is retried by the worker; the
// process stays pending (and counted) until the reap is established.
func TestFinalizeRetriesTransientWaitability(t *testing.T) {
	root := newScriptedRoot()
	root.waitable = make(chan waitableAnswer)
	proc := startScripted(t, root, true)
	countScripted(t, proc)
	before := UnfinalizedProcesses()
	root.exit <- scriptedExit{held: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = proc.Finalize(ctx)
	answerWaitable(t, root, waitableAnswer{waitable: false})
	answerWaitable(t, root, waitableAnswer{waitable: false})
	if reaps, count := root.reapCount(), UnfinalizedProcesses(); reaps != 0 || count != before {
		t.Fatalf("after negative answers: reaps %d, unfinalized %d (was %d)", reaps, count, before)
	}
	assertProcessDone(t, proc)
	answerWaitable(t, root, waitableAnswer{waitable: true})
	if err := finalize(t, proc); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if reaps, count := root.reapCount(), UnfinalizedProcesses(); reaps != 1 || count != before-1 {
		t.Fatalf("after the reap: reaps %d, unfinalized %d (was %d)", reaps, count, before)
	}
}

// When waitability can never be established (Linux without waitid), the
// root is never reaped: Finalize ends with ErrReleaseUnavailable carrying the
// query error, for every caller; signals stay refused and the process stays
// counted.
func TestFinalizeEndsWithReleaseUnavailable(t *testing.T) {
	root := newScriptedRoot()
	root.waitable = make(chan waitableAnswer)
	proc := startScripted(t, root, true)
	countScripted(t, proc)
	before := UnfinalizedProcesses()
	root.exit <- scriptedExit{held: true}
	queryErr := errors.New("waitid: ENOSYS")
	RequestFinalize(proc)
	answerWaitable(t, root, waitableAnswer{err: queryErr})
	for range 2 {
		if err := finalize(t, proc); !errors.Is(err, ErrReleaseUnavailable) || !errors.Is(err, queryErr) {
			t.Fatalf("Finalize without waitability = %v, want ErrReleaseUnavailable with the query error", err)
		}
	}
	if reaps, count := root.reapCount(), UnfinalizedProcesses(); reaps != 0 || count != before {
		t.Fatalf("reaps %d, unfinalized %d (was %d), want no reap and still counted", reaps, count, before)
	}
	assertProcessDone(t, proc)
}

// A platform that cannot hold the root reaps it at exit; group signals are
// refused from then on, and Finalize only records the end of ownership.
func TestRootReapedAtExitRefusesSignals(t *testing.T) {
	root := newScriptedRoot()
	proc := startScripted(t, root, true)
	if err := proc.KillTree(); err != nil {
		t.Fatalf("KillTree before exit: %v", err)
	}
	root.exit <- scriptedExit{result: Result{ExitCode: 7}}
	awaitSignal(t, proc.RootExited(), "root exit")
	assertProcessDone(t, proc)
	if err := finalize(t, proc); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	endPipes(proc)
	if result := waitResult(t, proc); result.ExitCode != 7 {
		t.Fatalf("Wait = %+v, want the exit reaped at exit", result)
	}
	if reaps := root.reapCount(); reaps != 0 {
		t.Fatalf("root reaped again %d times", reaps)
	}
	if got := root.sent(); !slices.Equal(got, []string{"kill"}) {
		t.Fatalf("signals sent = %v, want only the kill before exit", got)
	}
}

// Handle-based signals (Windows) cannot reach a reused number, so they are
// not gated; the handle owner reports completion itself.
func TestHandleSignalsAreNotGated(t *testing.T) {
	root := newScriptedRoot()
	proc := startScripted(t, root, false)
	root.exit <- scriptedExit{}
	endPipes(proc)
	waitResult(t, proc)
	if err := finalize(t, proc); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if err := proc.KillTree(); err != nil {
		t.Fatalf("KillTree after Finalize: %v", err)
	}
	if got := root.sent(); !slices.Equal(got, []string{"kill"}) {
		t.Fatalf("signals sent = %v, want the handle kill", got)
	}
}

// CleanupOwner: RootDone follows Wait while the root is still unreaped, so
// BeginCleanup's KillTree reaches the group; then it finalizes, and only then
// reports RootReaped and completes.
func TestCleanupOwnerFinalizesAfterKill(t *testing.T) {
	root := newScriptedRoot()
	proc := startScripted(t, root, true)
	owner := NewCleanupOwner(proc)
	root.exit <- scriptedExit{held: true}
	endPipes(proc)
	awaitSignal(t, owner.RootDone(), "Wait through the owner")
	if state := owner.Latest(); state.RootReaped || root.reapCount() != 0 {
		t.Fatalf("owner reaped before BeginCleanup: %+v", state)
	}
	owner.BeginCleanup()
	awaitSignal(t, owner.Done(), "cleanup completion")
	state := owner.Latest()
	if !state.RootReaped || !state.Complete || state.RootResult.ReleaseErr != nil {
		t.Fatalf("owner state = %+v, want the reap", state)
	}
	if got := root.sent(); !slices.Equal(got, []string{"kill"}) {
		t.Fatalf("signals sent = %v, want the cleanup kill before the reap", got)
	}
	assertProcessDone(t, proc)
}

// A failed release completes cleanup with its error and RootReaped false;
// signals stay refused.
func TestCleanupOwnerReportsFailedReap(t *testing.T) {
	root := newScriptedRoot()
	root.reapErr = errors.New("reap failed")
	proc := startScripted(t, root, true)
	owner := NewCleanupOwner(proc)
	root.exit <- scriptedExit{held: true}
	owner.BeginCleanup()
	awaitSignal(t, owner.Done(), "cleanup completion after the failed reap")
	state := owner.Latest()
	if state.RootReaped || !state.Complete || !errors.Is(state.RootResult.ReleaseErr, root.reapErr) {
		t.Fatalf("owner state = %+v, want completion with the reap failure and no reap", state)
	}
	assertProcessDone(t, proc)
}

// The Finalize helper falls back to Wait for a Process without Finalizer.
func TestFinalizeHelperFallsBackToWait(t *testing.T) {
	child := &heldProcess{release: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := Finalize(ctx, child); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Finalize before Wait = %v, want the context error", err)
	}
	close(child.release)
	if err := Finalize(context.Background(), child); err != nil {
		t.Fatalf("Finalize after Wait: %v", err)
	}
}

// An owner whose kill finalization refuses does not report termination as
// confirmed: the refusal stays the outcome unless an earlier kill's outcome
// was handed over, which then stands.
func TestCleanupOwnerDoesNotConfirmRefusedKill(t *testing.T) {
	denied := errors.New("kill denied")
	for _, tc := range []struct {
		name    string
		options []CleanupOption
		want    error
	}{
		{"no prior kill", nil, ErrSignalsFinalized},
		{"denied prior kill", []CleanupOption{WithPriorKill(denied)}, denied},
		{"delivered prior kill", []CleanupOption{WithPriorKill(nil)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newScriptedRoot()
			proc := startScripted(t, root, true)
			root.exit <- scriptedExit{held: true}
			if err := finalize(t, proc); err != nil {
				t.Fatalf("Finalize: %v", err)
			}
			owner := NewCleanupOwnerWithDrain(proc, closedChannel(), tc.options...)
			owner.BeginCleanup()
			awaitSignal(t, owner.Done(), "cleanup completion")
			state := owner.Latest()
			if tc.want == nil && state.TreeTerminationErr != nil || tc.want != nil && !errors.Is(state.TreeTerminationErr, tc.want) {
				t.Fatalf("TreeTerminationErr = %v, want %v", state.TreeTerminationErr, tc.want)
			}
			if sent := root.sent(); len(sent) != 0 {
				t.Fatalf("signals sent after finalization: %v", sent)
			}
		})
	}
}

func closedChannel() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
