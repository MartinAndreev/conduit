package opencode

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type closeOutcome struct {
	res provider.CloseSessionResult
	err error
}

func closeAsync(sess provider.Session) <-chan closeOutcome {
	ch := make(chan closeOutcome, 1)
	go func() {
		res, err := sess.Close(context.Background(), provider.CloseSessionRequest{
			Session: sess.Ref(),
			Policy:  provider.CloseInterruptThenClose,
		})
		ch <- closeOutcome{res: res, err: err}
	}()
	return ch
}

func awaitCloseOutcome(t *testing.T, ch <-chan closeOutcome, limit time.Duration) provider.CloseSessionResult {
	t.Helper()
	select {
	case out := <-ch:
		if out.err != nil {
			t.Fatalf("Close error = %v", out.err)
		}
		return out.res
	case <-time.After(limit):
		t.Fatalf("Close did not return within %s", limit)
		return provider.CloseSessionResult{}
	}
}

// cleanExitFinalizeProcess exits cleanly (code 0) right away and gates
// Finalize after signalling finalizeEntered, so a test can observe Close
// arriving while readRun is in the finalization window after its clean-exit
// group kill.
type cleanExitFinalizeProcess struct {
	rootDone        chan struct{}
	finalizeEntered chan struct{}
	finalizeGate    chan struct{}
	finalizeDone    chan struct{}
	enterOnce       sync.Once
	gateOnce        sync.Once
	doneOnce        sync.Once
	finalized       atomic.Bool
	kills           atomic.Int32
	finalizes       atomic.Int32
	killErr         error
	finalizeErr     error
}

func newCleanExitFinalizeProcess() *cleanExitFinalizeProcess {
	root := make(chan struct{})
	close(root)
	return &cleanExitFinalizeProcess{
		rootDone:        root,
		finalizeEntered: make(chan struct{}),
		finalizeGate:    make(chan struct{}),
		finalizeDone:    make(chan struct{}),
	}
}

func (*cleanExitFinalizeProcess) PID() int                      { return 31 }
func (*cleanExitFinalizeProcess) Stdin() io.WriteCloser         { return nopWriteCloser{Writer: io.Discard} }
func (*cleanExitFinalizeProcess) Stdout() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (*cleanExitFinalizeProcess) Stderr() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (*cleanExitFinalizeProcess) Interrupt() error              { return nil }
func (*cleanExitFinalizeProcess) TreeKillSupported() bool       { return true }
func (p *cleanExitFinalizeProcess) RootExited() <-chan struct{} { return p.rootDone }
func (*cleanExitFinalizeProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: 0}
}
func (*cleanExitFinalizeProcess) Wait() processruntime.Result {
	return processruntime.Result{ExitCode: 0}
}
func (p *cleanExitFinalizeProcess) KillTree() error {
	p.kills.Add(1)
	if p.finalized.Load() {
		return processruntime.ErrSignalsFinalized
	}
	return p.killErr
}
func (p *cleanExitFinalizeProcess) Finalize(ctx context.Context) error {
	p.finalized.Store(true)
	p.finalizes.Add(1)
	p.enterOnce.Do(func() { close(p.finalizeEntered) })
	select {
	case <-p.finalizeGate:
		p.doneOnce.Do(func() { close(p.finalizeDone) })
		return p.finalizeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *cleanExitFinalizeProcess) release() {
	p.gateOnce.Do(func() { close(p.finalizeGate) })
}

var (
	_ processruntime.Finalizer        = (*cleanExitFinalizeProcess)(nil)
	_ processruntime.RootExitObserver = (*cleanExitFinalizeProcess)(nil)
)

// finalizedKillProcess stays alive on root and stdout while KillTree returns
// ErrSignalsFinalized, as on fallback Unix platforms where the root was
// already reaped on exit.
type finalizedKillProcess struct {
	stdoutR  *io.PipeReader
	stdoutW  *io.PipeWriter
	done     chan struct{}
	once     sync.Once
	kills    atomic.Int32
	rootDone chan struct{}
}

func newFinalizedKillProcess() *finalizedKillProcess {
	r, w := io.Pipe()
	return &finalizedKillProcess{stdoutR: r, stdoutW: w, done: make(chan struct{}), rootDone: make(chan struct{})}
}

func (*finalizedKillProcess) PID() int                      { return 32 }
func (*finalizedKillProcess) Stdin() io.WriteCloser         { return nopWriteCloser{Writer: io.Discard} }
func (p *finalizedKillProcess) Stdout() io.ReadCloser       { return p.stdoutR }
func (*finalizedKillProcess) Stderr() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (*finalizedKillProcess) Interrupt() error              { return nil }
func (*finalizedKillProcess) TreeKillSupported() bool       { return true }
func (p *finalizedKillProcess) RootExited() <-chan struct{} { return p.rootDone }
func (*finalizedKillProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: 0}
}
func (p *finalizedKillProcess) Wait() processruntime.Result {
	<-p.done
	return processruntime.Result{ExitCode: 0}
}
func (p *finalizedKillProcess) KillTree() error {
	p.kills.Add(1)
	return processruntime.ErrSignalsFinalized
}
func (*finalizedKillProcess) Finalize(context.Context) error { return nil }
func (p *finalizedKillProcess) release() {
	p.once.Do(func() {
		_ = p.stdoutW.Close()
		close(p.rootDone)
		close(p.done)
	})
}

var (
	_ processruntime.Finalizer        = (*finalizedKillProcess)(nil)
	_ processruntime.RootExitObserver = (*finalizedKillProcess)(nil)
)

func openSessionWithGrace(t *testing.T, launcher processruntime.Launcher, servers *fakeServerLauncher, stopGrace time.Duration) (provider.Session, *fakeServerProcess) {
	t.Helper()
	if servers == nil {
		servers = &fakeServerLauncher{}
	}
	adapter := New(withFakeServer(Config{
		Control:        &fakeControl{},
		Launcher:       launcher,
		serverLauncher: servers,
		StopGrace:      stopGrace,
	}))
	req := openRequest()
	opened, err := adapter.OpenSession(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
		Session:        opened.Session.Ref(),
		Assignment:     req.Assignment,
		ResultContract: req.ResultContract,
	}); err != nil {
		t.Fatal(err)
	}
	srv, _ := servers.last()
	return opened.Session, srv
}

// Decision 2026-11-20 (Option C): every clean run exit sends the single
// group kill via runClient before finalization. Close confirms
// ResourcesReleased=true once that kill and release confirm (both after the
// turn ended and when Close catches the client in its finalization window),
// and returns false whenever the kill is refused or ErrSignalsFinalized
// whether the client is active in s.client or tracked in s.releases.
func TestCloseAwaitsCleanRunKillAndFinalization(t *testing.T) {
	t.Run("Close after normal clean turn confirms release with one kill", func(t *testing.T) {
		proc := newCleanExitFinalizeProcess()
		proc.release()
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, time.Second)
		awaitTurnEnded(t, sess)
		res := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
		if res.ExecutionState != provider.ExecutionNone || !res.ResourcesReleased {
			t.Fatalf("close after normal clean turn = %#v, want ExecutionNone and ResourcesReleased=true", res)
		}
		if kills := proc.kills.Load(); kills != 1 {
			t.Fatalf("kills = %d, want 1", kills)
		}
	})

	t.Run("Close in finalization window waits for finalize and confirms once done", func(t *testing.T) {
		proc := newCleanExitFinalizeProcess()
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, 40*time.Millisecond)
		client := sess.(*session).client
		select {
		case <-proc.finalizeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("readRun did not enter finalization after clean-exit kill")
		}
		// While finalizeGate is held, Close must wait out StopGrace and return
		// ResourcesReleased=false (proving Close genuinely awaits finalization).
		first := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
		if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
			t.Fatalf("first close while finalization held = %#v, want ExecutionNone and ResourcesReleased=false", first)
		}
		if kills := proc.kills.Load(); kills != 1 {
			t.Fatalf("kills = %d, want 1", kills)
		}
		// Release finalization: the shared observer completes and a repeated
		// Close confirms ResourcesReleased=true with still only 1 kill.
		proc.release()
		select {
		case <-client.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("shared release observer did not complete after finalize unblocked")
		}
		second := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
		if second.ExecutionState != provider.ExecutionNone || !second.ResourcesReleased {
			t.Fatalf("second close after finalization finished = %#v, want ExecutionNone and ResourcesReleased=true", second)
		}
		if kills := proc.kills.Load(); kills != 1 {
			t.Fatalf("kills = %d, want 1", kills)
		}
	})

	t.Run("failed clean-exit finalization stays unreleased", func(t *testing.T) {
		proc := newCleanExitFinalizeProcess()
		proc.finalizeErr = errors.New("reap failed")
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, time.Second)
		client := sess.(*session).client
		select {
		case <-proc.finalizeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("readRun did not enter finalization")
		}
		closed := closeAsync(sess)
		waitForState(t, sess, provider.SessionClosed)
		proc.release()
		select {
		case <-client.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("shared release observer did not finish")
		}
		first := awaitCloseOutcome(t, closed, 2*time.Second)
		second := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
		for i, res := range []provider.CloseSessionResult{first, second} {
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d after failed finalization = %#v", i+1, res)
			}
		}
		if kills := proc.kills.Load(); kills != 1 {
			t.Fatalf("kills = %d, want 1", kills)
		}
	})

	t.Run("unconfirmed clean-exit kill stays unreleased after readRun moves client to s.releases", func(t *testing.T) {
		for name, killErr := range map[string]error{
			"refused kill":        errors.New("kill denied"),
			"ErrSignalsFinalized": processruntime.ErrSignalsFinalized,
		} {
			t.Run(name, func(t *testing.T) {
				proc := newCleanExitFinalizeProcess()
				proc.killErr = killErr
				proc.release()
				sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, time.Second)
				awaitTurnEnded(t, sess)
				for i := range 2 {
					res := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
					if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
						t.Fatalf("close #%d with %s in s.releases = %#v, want ExecutionNone and ResourcesReleased=false", i+1, name, res)
					}
				}
				if kills := proc.kills.Load(); kills != 1 {
					t.Fatalf("kills = %d, want 1", kills)
				}
			})
		}
	})

	t.Run("ErrSignalsFinalized from Close KillTree on active client is never confirmed", func(t *testing.T) {
		proc := newFinalizedKillProcess()
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, 50*time.Millisecond)
		for i := range 2 {
			res := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d with ErrSignalsFinalized = %#v, want ResourcesReleased=false", i+1, res)
			}
		}
		if kills := proc.kills.Load(); kills != 1 {
			t.Fatalf("kills = %d, want 1", kills)
		}
	})
}

// countingHeldPipeProcess models a live run client whose KillTree succeeds
// (exiting the root) while an escaped descendant outside the group keeps
// stdout open until release(). waitEntered and waitExited synchronize
// deterministically without sleeps.
type countingHeldPipeProcess struct {
	stdoutR       *io.PipeReader
	stdoutW       *io.PipeWriter
	rootDone      chan struct{}
	rootOnce      sync.Once
	waitEntered   chan struct{}
	waitExited    chan struct{}
	waitGate      chan struct{}
	enterOnce     sync.Once
	exitOnce      sync.Once
	releaseOnce   sync.Once
	kills         atomic.Int32
	waits         atomic.Int32
	inFlightWaits atomic.Int32
	finalizes     atomic.Int32
}

func newCountingHeldPipeProcess() *countingHeldPipeProcess {
	r, w := io.Pipe()
	return &countingHeldPipeProcess{
		stdoutR:     r,
		stdoutW:     w,
		rootDone:    make(chan struct{}),
		waitEntered: make(chan struct{}),
		waitExited:  make(chan struct{}),
		waitGate:    make(chan struct{}),
	}
}

func (*countingHeldPipeProcess) PID() int                      { return 33 }
func (*countingHeldPipeProcess) Stdin() io.WriteCloser         { return nopWriteCloser{Writer: io.Discard} }
func (p *countingHeldPipeProcess) Stdout() io.ReadCloser       { return p.stdoutR }
func (*countingHeldPipeProcess) Stderr() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (*countingHeldPipeProcess) Interrupt() error              { return nil }
func (*countingHeldPipeProcess) TreeKillSupported() bool       { return true }
func (p *countingHeldPipeProcess) RootExited() <-chan struct{} { return p.rootDone }
func (*countingHeldPipeProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}
func (p *countingHeldPipeProcess) KillTree() error {
	p.kills.Add(1)
	p.rootOnce.Do(func() { close(p.rootDone) })
	return nil
}
func (p *countingHeldPipeProcess) Finalize(context.Context) error {
	p.finalizes.Add(1)
	return nil
}
func (p *countingHeldPipeProcess) Wait() processruntime.Result {
	p.waits.Add(1)
	p.inFlightWaits.Add(1)
	p.enterOnce.Do(func() { close(p.waitEntered) })
	defer func() {
		p.inFlightWaits.Add(-1)
		p.exitOnce.Do(func() { close(p.waitExited) })
	}()
	<-p.waitGate
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}
func (p *countingHeldPipeProcess) release() {
	p.releaseOnce.Do(func() {
		p.rootOnce.Do(func() { close(p.rootDone) })
		_ = p.stdoutW.Close()
		close(p.waitGate)
	})
}

var (
	_ processruntime.Finalizer        = (*countingHeldPipeProcess)(nil)
	_ processruntime.RootExitObserver = (*countingHeldPipeProcess)(nil)
)

// Limit 2: repeated Close timeouts while an escaped descendant holds a pipe
// must share one Wait observer instead of spawning a new Wait goroutine per
// Close call.
func TestRepeatedCloseTimeoutsShareSingleWaitObserver(t *testing.T) {
	proc := newCountingHeldPipeProcess()
	t.Cleanup(proc.release)
	sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, 30*time.Millisecond)
	client := sess.(*session).client
	for i := range 5 {
		res := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
		if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
			t.Fatalf("close timeout #%d = %#v, want ExecutionNone and ResourcesReleased=false", i+1, res)
		}
		if i == 0 {
			select {
			case <-proc.waitEntered:
			case <-time.After(2 * time.Second):
				t.Fatal("shared Wait observer did not start")
			}
		}
	}
	if kills := proc.kills.Load(); kills != 1 {
		t.Fatalf("kills = %d, want 1", kills)
	}
	if waits := proc.waits.Load(); waits != 1 {
		t.Fatalf("Wait calls = %d after 5 timed-out Close calls, want 1", waits)
	}
	if inFlight := proc.inFlightWaits.Load(); inFlight != 1 {
		t.Fatalf("in-flight Wait calls = %d, want 1", inFlight)
	}
	proc.release()
	select {
	case <-proc.waitExited:
	case <-time.After(2 * time.Second):
		t.Fatal("shared Wait observer did not exit after pipe release")
	}
	select {
	case <-client.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("shared release observer did not complete after pipe release")
	}
	final := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
	if final.ExecutionState != provider.ExecutionNone || !final.ResourcesReleased {
		t.Fatalf("close after pipe release = %#v, want ExecutionNone and ResourcesReleased=true", final)
	}
	if waits := proc.waits.Load(); waits != 1 || proc.kills.Load() != 1 {
		t.Fatalf("after recovery waits=%d kills=%d, want 1 and 1", waits, proc.kills.Load())
	}
}

// serverLossHeldPipeProcess models a live run client whose root stays alive
// until watchServer kills it, while an escaped descendant keeps stderr open
// after KillTree so Wait stays blocked until release().
type serverLossHeldPipeProcess struct {
	stderrR        *io.PipeReader
	stderrW        *io.PipeWriter
	rootDone       chan struct{}
	rootOnce       sync.Once
	waitGate       chan struct{}
	finalizeCalled chan struct{}
	finalizeOnce   sync.Once
	releaseOnce    sync.Once
	waited         atomic.Bool
	finalized      atomic.Bool
	kills          atomic.Int32
	finalizes      atomic.Int32
	killErr        error
}

func newServerLossHeldPipeProcess(killErr error) *serverLossHeldPipeProcess {
	r, w := io.Pipe()
	return &serverLossHeldPipeProcess{
		stderrR:        r,
		stderrW:        w,
		rootDone:       make(chan struct{}),
		waitGate:       make(chan struct{}),
		finalizeCalled: make(chan struct{}),
		killErr:        killErr,
	}
}

func (*serverLossHeldPipeProcess) PID() int                      { return 34 }
func (*serverLossHeldPipeProcess) Stdin() io.WriteCloser         { return nopWriteCloser{Writer: io.Discard} }
func (*serverLossHeldPipeProcess) Stdout() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (p *serverLossHeldPipeProcess) Stderr() io.ReadCloser       { return p.stderrR }
func (*serverLossHeldPipeProcess) Interrupt() error              { return nil }
func (*serverLossHeldPipeProcess) TreeKillSupported() bool       { return true }
func (p *serverLossHeldPipeProcess) RootExited() <-chan struct{} { return p.rootDone }
func (*serverLossHeldPipeProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}
func (p *serverLossHeldPipeProcess) KillTree() error {
	p.kills.Add(1)
	if p.finalized.Load() {
		return processruntime.ErrSignalsFinalized
	}
	if p.killErr == nil {
		p.rootOnce.Do(func() { close(p.rootDone) })
	}
	return p.killErr
}
func (p *serverLossHeldPipeProcess) Finalize(context.Context) error {
	p.finalized.Store(true)
	p.finalizes.Add(1)
	p.finalizeOnce.Do(func() { close(p.finalizeCalled) })
	return nil
}
func (p *serverLossHeldPipeProcess) Wait() processruntime.Result {
	<-p.waitGate
	p.waited.Store(true)
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}
func (p *serverLossHeldPipeProcess) release() {
	p.releaseOnce.Do(func() {
		p.rootOnce.Do(func() { close(p.rootDone) })
		_ = p.stderrW.Close()
		close(p.waitGate)
	})
}

var (
	_ processruntime.Finalizer        = (*serverLossHeldPipeProcess)(nil)
	_ processruntime.RootExitObserver = (*serverLossHeldPipeProcess)(nil)
)

// Limit 3: on server loss, watchServer terminates the active run client
// through the shared owner and registers finalization right after its kill,
// even while a descendant still holds stderr so Wait is blocked. Later Close
// reuses that single kill's outcome without sending another kill.
func TestServerLossTerminatesAndFinalizesRunningClientWithHeldPipe(t *testing.T) {
	for _, tc := range []struct {
		name         string
		killErr      error
		wantReleased bool
	}{
		{"confirmed server-loss kill", nil, true},
		{"refused server-loss kill", errors.New("kill denied"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc := newServerLossHeldPipeProcess(tc.killErr)
			t.Cleanup(proc.release)
			sess, srv := openSessionWithGrace(t, oneRunLauncher{proc}, &fakeServerLauncher{}, 50*time.Millisecond)
			if proc.kills.Load() != 0 || proc.finalized.Load() {
				t.Fatalf("live run client killed=%d finalized=%t before server loss", proc.kills.Load(), proc.finalized.Load())
			}
			srv.exit()
			select {
			case <-proc.finalizeCalled:
			case <-time.After(2 * time.Second):
				t.Fatal("watchServer did not register finalization after killing the run client")
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("kills after server loss = %d, want 1", kills)
			}
			if proc.waited.Load() {
				t.Fatal("Wait returned although the descendant still holds stderr")
			}
			pending := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
			if pending.ExecutionState != provider.ExecutionNone || pending.ResourcesReleased {
				t.Fatalf("close while pipe held = %#v", pending)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("Close sent a second kill after watchServer: kills = %d", kills)
			}
			proc.release()
			done := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
			if done.ExecutionState != provider.ExecutionNone || done.ResourcesReleased != tc.wantReleased {
				t.Fatalf("close after pipe release = %#v, want ExecutionNone and ResourcesReleased=%t", done, tc.wantReleased)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("kills after final close = %d, want 1", kills)
			}
		})
	}
}

type closeErrReader struct {
	io.ReadCloser
	closeErr error
}

func (r closeErrReader) Close() error {
	return errors.Join(r.ReadCloser.Close(), r.closeErr)
}

// gatedReadCloser blocks Close until gate closes, signalling entered first.
type gatedReadCloser struct {
	io.Reader
	entered   chan struct{}
	gate      chan struct{}
	enterOnce sync.Once
	gateOnce  sync.Once
	err       error
}

func newGatedReadCloser(err error) *gatedReadCloser {
	return &gatedReadCloser{
		Reader:  strings.NewReader(""),
		entered: make(chan struct{}),
		gate:    make(chan struct{}),
		err:     err,
	}
}
func (r *gatedReadCloser) Close() error {
	r.enterOnce.Do(func() { close(r.entered) })
	<-r.gate
	return r.err
}
func (r *gatedReadCloser) release() { r.gateOnce.Do(func() { close(r.gate) }) }

// failingReleaseServerProcess wraps fakeServerProcess to inject Wait's
// ReleaseErr, stdout/stderr PipeCloseErr, or StdinCloseErr while still
// confirming group termination and root reap.
type failingReleaseServerProcess struct {
	*fakeServerProcess
	waitReleaseErr error
	stdoutCloseErr error
	stderrCloseErr error
	stdinCloseErr  error
	waitGate       chan struct{}
}

func (p *failingReleaseServerProcess) Stdin() io.WriteCloser {
	if p.stdinCloseErr != nil {
		return failingCloser{err: p.stdinCloseErr}
	}
	return p.fakeServerProcess.Stdin()
}
func (p *failingReleaseServerProcess) Stdout() io.ReadCloser {
	if p.stdoutCloseErr != nil {
		return closeErrReader{ReadCloser: p.fakeServerProcess.Stdout(), closeErr: p.stdoutCloseErr}
	}
	return p.fakeServerProcess.Stdout()
}
func (p *failingReleaseServerProcess) Stderr() io.ReadCloser {
	if p.stderrCloseErr != nil {
		return closeErrReader{ReadCloser: p.fakeServerProcess.Stderr(), closeErr: p.stderrCloseErr}
	}
	return p.fakeServerProcess.Stderr()
}
func (p *failingReleaseServerProcess) Wait() processruntime.Result {
	if p.waitGate != nil {
		<-p.waitGate
	}
	res := p.fakeServerProcess.Wait()
	res.ReleaseErr = p.waitReleaseErr
	return res
}

type failingReleaseServerLauncher struct {
	script    string
	configure func(*failingReleaseServerProcess)
}

func (l failingReleaseServerLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	script := l.script
	if script == "" {
		script = testListeningLine
	}
	proc := &failingReleaseServerProcess{fakeServerProcess: newFakeServerProcess(script)}
	if l.configure != nil {
		l.configure(proc)
	}
	return proc, nil
}

// Limit 4: server stop must evaluate release errors (Wait().ReleaseErr,
// PipeCloseErr, StdinCloseErr) through rules.DecideRelease. Close still
// reports ExecutionNone when the server group stop is confirmed, but keeps
// ResourcesReleased false.
func TestServerReleaseErrorsBlockResourcesReleased(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*failingReleaseServerProcess)
	}{
		{"Wait ReleaseErr", func(p *failingReleaseServerProcess) { p.waitReleaseErr = errors.New("job object close failed") }},
		{"stdout PipeCloseErr", func(p *failingReleaseServerProcess) { p.stdoutCloseErr = errors.New("stdout close failed") }},
		{"stderr PipeCloseErr", func(p *failingReleaseServerProcess) { p.stderrCloseErr = errors.New("stderr close failed") }},
		{"StdinCloseErr", func(p *failingReleaseServerProcess) { p.stdinCloseErr = errors.New("stdin close failed") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := New(withFakeServer(Config{
				Control:        &fakeControl{},
				Launcher:       &fakeLauncher{},
				serverLauncher: failingReleaseServerLauncher{configure: tc.configure},
				StopGrace:      200 * time.Millisecond,
			}))
			opened, err := adapter.OpenSession(context.Background(), openRequest())
			if err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				res, err := opened.Session.Close(context.Background(), provider.CloseSessionRequest{
					Session: opened.Session.Ref(),
					Policy:  provider.CloseRequireQuiescent,
				})
				if err != nil {
					t.Fatalf("close #%d error = %v", i+1, err)
				}
				if !res.HandleClosed || res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
					t.Fatalf("close #%d = %#v, want HandleClosed=true ExecutionNone ResourcesReleased=false", i+1, res)
				}
			}
		})
	}

	t.Run("failed open joins server release error", func(t *testing.T) {
		releaseErr := errors.New("server job object close failed")
		adapter := New(withFakeServer(Config{
			Control: &failingCreateControl{sent: true, err: errors.New("create failed")},
			serverLauncher: failingReleaseServerLauncher{configure: func(p *failingReleaseServerProcess) {
				p.waitReleaseErr = releaseErr
			}},
			StopGrace: 200 * time.Millisecond,
		}))
		_, err := adapter.OpenSession(context.Background(), openRequest())
		if !errors.Is(err, releaseErr) {
			t.Fatalf("failed open error = %v, want joined server release error %v", err, releaseErr)
		}
	})
}

// gatedCloser blocks Close until gate closes, signalling entered once inside.
type gatedCloser struct {
	entered   chan struct{}
	gate      chan struct{}
	enterOnce sync.Once
	gateOnce  sync.Once
	err       error
}

func newGatedCloser(err error) *gatedCloser {
	return &gatedCloser{entered: make(chan struct{}), gate: make(chan struct{}), err: err}
}
func (*gatedCloser) Write(b []byte) (int, error) { return len(b), nil }
func (c *gatedCloser) Close() error {
	c.enterOnce.Do(func() { close(c.entered) })
	<-c.gate
	return c.err
}
func (c *gatedCloser) release() { c.gateOnce.Do(func() { close(c.gate) }) }

// stdinAccountingProcess models a run client whose stdin close, KillTree,
// forced pipe close, Wait, and root exit can be independently scripted and
// gated.
type stdinAccountingProcess struct {
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	stderr         io.ReadCloser
	killErr        error
	killEntered    chan struct{}
	killGate       chan struct{}
	killEnterOnce  sync.Once
	killGateOnce   sync.Once
	kills          atomic.Int32
	waitGate       chan struct{}
	waitDone       chan struct{}
	waitOnce       sync.Once
	waitDoneOnce   sync.Once
	exitCode       int
	finalizeErr    error
	treeKillUnsupp bool
}

func newStdinAccountingProcess(stdin io.WriteCloser) *stdinAccountingProcess {
	return &stdinAccountingProcess{
		stdin:       stdin,
		stdout:      io.NopCloser(strings.NewReader("")),
		stderr:      io.NopCloser(strings.NewReader("")),
		killEntered: make(chan struct{}),
		waitDone:    make(chan struct{}),
	}
}

func (*stdinAccountingProcess) PID() int                { return 35 }
func (p *stdinAccountingProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *stdinAccountingProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *stdinAccountingProcess) Stderr() io.ReadCloser { return p.stderr }
func (*stdinAccountingProcess) Interrupt() error        { return nil }
func (p *stdinAccountingProcess) TreeKillSupported() bool {
	return !p.treeKillUnsupp
}
func (p *stdinAccountingProcess) KillTree() error {
	p.kills.Add(1)
	p.killEnterOnce.Do(func() { close(p.killEntered) })
	if p.killGate != nil {
		<-p.killGate
	}
	return p.killErr
}
func (p *stdinAccountingProcess) Wait() processruntime.Result {
	if p.waitGate != nil {
		<-p.waitGate
	}
	p.waitDoneOnce.Do(func() { close(p.waitDone) })
	return processruntime.Result{ExitCode: p.exitCode}
}
func (p *stdinAccountingProcess) Finalize(context.Context) error { return p.finalizeErr }
func (p *stdinAccountingProcess) releaseWait() {
	if p.waitGate != nil {
		p.waitOnce.Do(func() { close(p.waitGate) })
	}
}
func (p *stdinAccountingProcess) releaseKill() {
	if p.killGate != nil {
		p.killGateOnce.Do(func() { close(p.killGate) })
	}
}

var _ processruntime.Finalizer = (*stdinAccountingProcess)(nil)

// Finding 1 (both rounds): stdin close failure retains kill outcome, stdin
// error, and forced pipe-close errors in runClient; forced pipe closure runs
// in its own worker whose completion enters releaseDone so StartTurn stays
// bounded even when pipe Close blocks; and Close cannot observe confirmed
// release while StartTurn's stdin close is still pending.
func TestStdinCloseFailureAccountingAndPendingWindow(t *testing.T) {
	t.Run("refused kill stays unreleased after root exits and s.client clears", func(t *testing.T) {
		proc := newStdinAccountingProcess(failingCloser{err: errors.New("stdin close failed")})
		proc.killErr = errors.New("kill denied")
		adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: oneRunLauncher{proc}, StopGrace: time.Second}))
		req := openRequest()
		opened, err := adapter.OpenSession(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
			Session: opened.Session.Ref(), Assignment: req.Assignment, ResultContract: req.ResultContract,
		}); err == nil {
			t.Fatal("StartTurn succeeded despite stdin close failure")
		}
		s := opened.Session.(*session)
		select {
		case <-proc.waitDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Wait did not finish")
		}
		for i := range 2 {
			res := awaitCloseOutcome(t, closeAsync(opened.Session), 2*time.Second)
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d after refused kill on stdin failure = %#v, want ExecutionNone and ResourcesReleased=false", i+1, res)
			}
		}
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if s.client != nil {
				t.Errorf("s.client was not cleared after Wait/Finalize finished")
			}
			return nil
		})
	})

	t.Run("stdin error alone blocks ResourcesReleased even when kill and reap succeed", func(t *testing.T) {
		proc := newStdinAccountingProcess(failingCloser{err: errors.New("stdin close failed")})
		adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: oneRunLauncher{proc}, StopGrace: time.Second}))
		req := openRequest()
		opened, err := adapter.OpenSession(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
			Session: opened.Session.Ref(), Assignment: req.Assignment, ResultContract: req.ResultContract,
		}); err == nil {
			t.Fatal("StartTurn succeeded despite stdin close failure")
		}
		for i := range 2 {
			res := awaitCloseOutcome(t, closeAsync(opened.Session), 2*time.Second)
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d after stdin close error = %#v, want ExecutionNone and ResourcesReleased=false", i+1, res)
			}
		}
	})

	t.Run("releaseDone waits for forced pipe-close worker and StartTurn stays bounded", func(t *testing.T) {
		stdoutErr := errors.New("stdout close failed")
		gatedStdout := newGatedReadCloser(stdoutErr)
		t.Cleanup(gatedStdout.release)
		proc := newStdinAccountingProcess(failingCloser{err: errors.New("stdin close failed")})
		proc.killErr = errors.New("kill denied")
		proc.stdout = gatedStdout
		adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: oneRunLauncher{proc}, StopGrace: 40 * time.Millisecond}))
		req := openRequest()
		opened, err := adapter.OpenSession(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		startErrCh := make(chan error, 1)
		go func() {
			_, startErr := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
				Session: opened.Session.Ref(), Assignment: req.Assignment, ResultContract: req.ResultContract,
			})
			startErrCh <- startErr
		}()
		select {
		case <-gatedStdout.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("forced pipe-close worker did not enter Stdout().Close()")
		}
		// StartTurn must return within its StopGrace budget even though
		// Stdout().Close() is still blocked.
		select {
		case startErr := <-startErrCh:
			if startErr == nil {
				t.Fatal("StartTurn succeeded despite stdin close failure")
			}
		case <-time.After(time.Second):
			t.Fatal("StartTurn blocked on Stdout().Close() beyond its cleanup budget")
		}
		s := opened.Session.(*session)
		var tracked *runClient
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			for _, rel := range s.releases {
				if rc, ok := rel.(*runClient); ok {
					tracked = rc
				}
			}
			return nil
		})
		if tracked == nil {
			t.Fatal("runClient was not tracked in s.releases")
		}
		// Wait and Finalize have already finished, but releaseDone must stay
		// open until the forced pipe-close worker completes.
		select {
		case <-proc.waitDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Wait did not finish")
		}
		select {
		case <-tracked.Done():
			t.Fatal("releaseDone closed before forced pipe-close worker finished")
		default:
		}
		gatedStdout.release()
		select {
		case <-tracked.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("releaseDone did not close after forced pipe-close worker finished")
		}
		if facts := tracked.releaseFacts(); !facts.PipeCloseFailed || !errors.Is(tracked.pipeCloseError(), stdoutErr) {
			t.Fatalf("releaseFacts = %#v pipeCloseErr = %v, want PipeCloseFailed=true with stdoutErr", facts, tracked.pipeCloseError())
		}
		if res := awaitCloseOutcome(t, closeAsync(opened.Session), 2*time.Second); res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
			t.Fatalf("close = %#v, want ExecutionNone and ResourcesReleased=false", res)
		}
	})

	t.Run("Close waits for pending StartTurn stdin close before deciding release", func(t *testing.T) {
		stdin := newGatedCloser(errors.New("stdin close failed"))
		t.Cleanup(stdin.release)
		proc := newStdinAccountingProcess(stdin)
		adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: oneRunLauncher{proc}, StopGrace: 40 * time.Millisecond}))
		req := openRequest()
		opened, err := adapter.OpenSession(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		startDone := make(chan error, 1)
		go func() {
			_, startErr := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
				Session: opened.Session.Ref(), Assignment: req.Assignment, ResultContract: req.ResultContract,
			})
			startDone <- startErr
		}()
		select {
		case <-stdin.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("StartTurn did not enter Stdin().Close()")
		}
		client := opened.Session.(*session).client
		// First Close: KillTree and Wait both finish immediately while
		// Stdin().Close() stays blocked; Close must time out after StopGrace
		// with ResourcesReleased=false instead of reporting premature release.
		first := awaitCloseOutcome(t, closeAsync(opened.Session), 2*time.Second)
		select {
		case <-proc.killEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("Close did not start KillTree")
		}
		select {
		case <-proc.waitDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Wait did not complete")
		}
		if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
			t.Fatalf("first Close while stdin pending = %#v, want ExecutionNone and ResourcesReleased=false", first)
		}
		stdin.release()
		select {
		case startErr := <-startDone:
			if startErr == nil {
				t.Fatal("StartTurn succeeded despite stdin close error")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("StartTurn did not return after stdin released")
		}
		select {
		case <-client.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("client release observer did not finish after stdin released")
		}
		second := awaitCloseOutcome(t, closeAsync(opened.Session), 2*time.Second)
		if second.ExecutionState != provider.ExecutionNone || second.ResourcesReleased {
			t.Fatalf("second Close after stdin error = %#v, want ExecutionNone and ResourcesReleased=false", second)
		}
	})
}

// Finding 2 (Close): a blocked KillTree must not block Close beyond StopGrace
// or prevent Close from stopping the server. Uses an async watchdog so a
// regression fails immediately instead of hanging the test binary.
func TestBlockedKillTreeDoesNotBlockCloseFromStoppingServer(t *testing.T) {
	proc := newStdinAccountingProcess(nopWriteCloser{Writer: io.Discard})
	proc.killGate = make(chan struct{})
	proc.waitGate = make(chan struct{})
	t.Cleanup(func() {
		proc.releaseKill()
		proc.releaseWait()
	})
	servers := &fakeServerLauncher{}
	sess, srv := openSessionWithGrace(t, oneRunLauncher{proc}, servers, 40*time.Millisecond)
	client := sess.(*session).client

	first := awaitCloseOutcome(t, closeAsync(sess), time.Second)
	select {
	case <-proc.killEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("kill worker did not invoke KillTree")
	}
	if srv.kills.Load() == 0 {
		t.Fatal("Close did not stop the server while client KillTree was blocked")
	}
	if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
		t.Fatalf("first Close = %#v, want ExecutionNone and ResourcesReleased=false", first)
	}

	proc.releaseKill()
	proc.releaseWait()
	select {
	case <-client.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("shared release observer did not complete after KillTree unblocked")
	}
	second := awaitCloseOutcome(t, closeAsync(sess), time.Second)
	if second.ExecutionState != provider.ExecutionNone || !second.ResourcesReleased {
		t.Fatalf("second Close after KillTree unblocked = %#v, want ExecutionNone and ResourcesReleased=true", second)
	}
	if kills := proc.kills.Load(); kills != 1 {
		t.Fatalf("kills = %d, want 1", kills)
	}
}

// Finding 2 (readRun): a blocked KillTree on either a failed or a clean root
// exit must not block watchRunRootExit or readRun beyond StopGrace; readRun
// tracks the client in s.releases and publishes its final state event.
func TestBlockedKillTreeDoesNotBlockReadRun(t *testing.T) {
	for _, tc := range []struct {
		name      string
		exitCode  int
		wantState provider.SessionState
	}{
		{"failed root exit", 1, provider.SessionLost},
		{"clean root exit", 0, provider.SessionIdleObserved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc := newStdinAccountingProcess(nopWriteCloser{Writer: io.Discard})
			proc.exitCode = tc.exitCode
			proc.killGate = make(chan struct{})
			t.Cleanup(proc.releaseKill)

			adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: oneRunLauncher{proc}, StopGrace: 40 * time.Millisecond}))
			req := openRequest()
			opened, err := adapter.OpenSession(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := opened.Session.Events(context.Background(), provider.EventCursor{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
				Session: opened.Session.Ref(), Assignment: req.Assignment, ResultContract: req.ResultContract,
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-proc.killEntered:
			case <-time.After(2 * time.Second):
				t.Fatal("run exit did not start the single kill worker")
			}
			// readRun must reach its final state event within a bounded wait
			// even while KillTree stays blocked, and emit opencode_run_release.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			warned := false
			reached := false
			for !reached {
				ev, err := stream.Receive(ctx)
				if err != nil {
					t.Fatalf("readRun blocked on KillTree (warned=%t): %v", warned, err)
				}
				switch body := ev.Body().(type) {
				case provider.ProtocolWarningEvent:
					if body.Code == "opencode_run_release" {
						warned = true
					}
				case provider.SessionStateEvent:
					if body.Current == tc.wantState {
						reached = true
					}
				}
			}
			if !warned {
				t.Fatal("readRun reached final state without opencode_run_release warning")
			}
			// While KillTree is still blocked, Close reports unreleased.
			first := awaitCloseOutcome(t, closeAsync(opened.Session), time.Second)
			if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
				t.Fatalf("close while KillTree blocked = %#v, want ExecutionNone and ResourcesReleased=false", first)
			}
			// Unblock KillTree: the tracked release completes and repeated
			// Close confirms release with still 1 kill.
			proc.releaseKill()
			s := opened.Session.(*session)
			var tracked processRelease
			_ = lockscope.WithMutexLock(&s.mu, func() error {
				if s.client != nil {
					tracked = s.client
				} else if len(s.releases) > 0 {
					tracked = s.releases[0]
				}
				return nil
			})
			if tracked == nil {
				t.Fatal("client release was neither active nor tracked")
			}
			select {
			case <-tracked.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("release observer did not finish after KillTree unblocked")
			}
			second := awaitCloseOutcome(t, closeAsync(opened.Session), time.Second)
			if second.ExecutionState != provider.ExecutionNone || !second.ResourcesReleased {
				t.Fatalf("close after KillTree unblocked = %#v, want ExecutionNone and ResourcesReleased=true", second)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("kills = %d, want 1", kills)
			}
		})
	}
}

// Finding 3 (previous round): even when KillTree is refused or tree kill is
// unsupported, Close must still start the shared release observer so
// releaseDone closes once the process eventually exits, while
// DecideClientCloseRelease keeps ResourcesReleased false.
func TestUnconfirmedKillStillCompletesReleaseObserver(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*stdinAccountingProcess)
	}{
		{"refused kill", func(p *stdinAccountingProcess) { p.killErr = errors.New("kill denied") }},
		{"tree kill unsupported", func(p *stdinAccountingProcess) { p.treeKillUnsupp = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdoutR, stdoutW := io.Pipe()
			proc := newStdinAccountingProcess(nopWriteCloser{Writer: io.Discard})
			proc.stdout = stdoutR
			proc.waitGate = make(chan struct{})
			tc.configure(proc)
			t.Cleanup(func() {
				_ = stdoutW.Close()
				proc.releaseWait()
			})
			sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, 50*time.Millisecond)
			client := sess.(*session).client
			first := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
			if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
				t.Fatalf("first close = %#v, want ExecutionNone and ResourcesReleased=false", first)
			}
			_ = stdoutW.Close()
			proc.releaseWait()
			select {
			case <-client.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("shared release observer never closed releaseDone after unconfirmed kill")
			}
			second := awaitCloseOutcome(t, closeAsync(sess), 2*time.Second)
			if second.ExecutionState != provider.ExecutionNone || second.ResourcesReleased {
				t.Fatalf("second close = %#v, want ExecutionNone and ResourcesReleased=false", second)
			}
		})
	}
}

// Finding 4: if server cleanup finishes right after Await timed out and
// before abortServer checks cleanup.Done, abortServerOutcome must recompute
// both stop and release decisions from cleanup.Latest() so ReleaseErr and
// pipe close errors are never lost (without any test seam field on ownedServer).
func TestAbortServerReadsFinalSnapshotWhenCleanupCompletesAfterAwaitTimeout(t *testing.T) {
	t.Run("preserves ReleaseErr and PipeCloseErr from final snapshot", func(t *testing.T) {
		waitReleaseErr := errors.New("late server job object close failed")
		stdoutCloseErr := errors.New("late server stdout close failed")
		waitGate := make(chan struct{})
		proc := &failingReleaseServerProcess{
			fakeServerProcess: newFakeServerProcess(testListeningLine),
			waitReleaseErr:    waitReleaseErr,
			stdoutCloseErr:    stdoutCloseErr,
			waitGate:          waitGate,
		}
		t.Cleanup(func() {
			select {
			case <-waitGate:
			default:
				close(waitGate)
			}
		})
		server, err := startOwnedServer(context.Background(), oneRunLauncher{proc}, linuxLaunch(), testServerPassword, time.Second, 20*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		timedOut := server.stopOutcome(context.Background())
		close(waitGate)
		select {
		case <-server.cleanup.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("server cleanup did not complete")
		}
		cause := errors.New("create session failed")
		got := abortServerOutcome(server, timedOut, cause)
		var pending *CleanupPendingError
		if errors.As(got, &pending) {
			t.Fatalf("abortServerOutcome returned CleanupPendingError although Done was closed: %v", got)
		}
		if !errors.Is(got, cause) || !errors.Is(got, waitReleaseErr) || !errors.Is(got, stdoutCloseErr) {
			t.Fatalf("abortServerOutcome = %v, want cause + waitReleaseErr + stdoutCloseErr", got)
		}
	})

	t.Run("clean completion after timeout returns original cause without pending error", func(t *testing.T) {
		waitGate := make(chan struct{})
		proc := &failingReleaseServerProcess{
			fakeServerProcess: newFakeServerProcess(testListeningLine),
			waitGate:          waitGate,
		}
		t.Cleanup(func() {
			select {
			case <-waitGate:
			default:
				close(waitGate)
			}
		})
		server, err := startOwnedServer(context.Background(), oneRunLauncher{proc}, linuxLaunch(), testServerPassword, time.Second, 20*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		timedOut := server.stopOutcome(context.Background())
		close(waitGate)
		select {
		case <-server.cleanup.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("server cleanup did not complete")
		}
		cause := errors.New("create session failed")
		got := abortServerOutcome(server, timedOut, cause)
		var pending *CleanupPendingError
		if errors.As(got, &pending) {
			t.Fatalf("abortServerOutcome returned CleanupPendingError on clean final snapshot: %v", got)
		}
		if got != cause {
			t.Fatalf("abortServerOutcome = %v, want unmodified cause %v", got, cause)
		}
	})
}

type scriptedControlProcess struct {
	stdin           io.WriteCloser
	output          string
	finalizeEntered chan struct{}
	finalizeOnce    sync.Once
}

func (*scriptedControlProcess) PID() int                { return 36 }
func (p *scriptedControlProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *scriptedControlProcess) Stdout() io.ReadCloser {
	return io.NopCloser(strings.NewReader(p.output))
}
func (*scriptedControlProcess) Stderr() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (*scriptedControlProcess) Interrupt() error        { return nil }
func (*scriptedControlProcess) KillTree() error         { return nil }
func (*scriptedControlProcess) TreeKillSupported() bool { return true }
func (*scriptedControlProcess) Wait() processruntime.Result {
	return processruntime.Result{ExitCode: 0}
}
func (p *scriptedControlProcess) Finalize(context.Context) error {
	if p.finalizeEntered != nil {
		p.finalizeOnce.Do(func() { close(p.finalizeEntered) })
	}
	return nil
}

var _ processruntime.Finalizer = (*scriptedControlProcess)(nil)

// Finding 3 + Finding 5 (control stdin without polling): the normal completion
// path of cliControl.run observes CleanupOwner's stdin close through a channel
// signal without polling, and hands any unconfirmed release to endControlOp.
func TestFinishedControlCommandAccountsForStdinClose(t *testing.T) {
	t.Run("failed stdin close fails command and blocks Close ResourcesReleased", func(t *testing.T) {
		stdinErr := errors.New("control stdin close failed")
		proc := &scriptedControlProcess{
			stdin:  failingCloser{err: stdinErr},
			output: `{"data":{"id":"receipt"}}`,
		}
		control := cliPromptControl{
			fakeControl: &fakeControl{},
			cli:         &cliControl{executable: "opencode", launcher: oneRunLauncher{proc}},
		}
		fixture := newReconcileFixture(t, control, 300*time.Millisecond, func(s *session) {
			s.config.StopGrace = 300 * time.Millisecond
		})
		err := steerOnce(context.Background(), fixture)
		if !errors.Is(err, errControlCleanup) || !errors.Is(err, stdinErr) {
			t.Fatalf("steer = %v, want errControlCleanup + stdinErr", err)
		}
		awaitTurnEnded(t, fixture.session)
		for i := range 2 {
			res := awaitCloseOutcome(t, closeAsync(fixture.session), 2*time.Second)
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d after failed control stdin close = %#v, want ExecutionNone and ResourcesReleased=false", i+1, res)
			}
		}
	})

	t.Run("pending stdin close stays tracked until stdin closes cleanly", func(t *testing.T) {
		stdin := newGatedCloser(nil)
		t.Cleanup(stdin.release)
		proc := &scriptedControlProcess{
			stdin:           stdin,
			output:          `{"data":{"id":"receipt"}}`,
			finalizeEntered: make(chan struct{}),
		}
		control := cliPromptControl{
			fakeControl: &fakeControl{},
			cli:         &cliControl{executable: "opencode", launcher: oneRunLauncher{proc}},
		}
		fixture := newReconcileFixture(t, control, 300*time.Millisecond, func(s *session) {
			s.config.StopGrace = 60 * time.Millisecond
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		steerErrCh := make(chan error, 1)
		go func() {
			steerErrCh <- steerOnce(ctx, fixture)
		}()
		// Cancel deterministically only after Finalize has been entered and
		// Stdin().Close() is blocked in CleanupOwner.
		select {
		case <-proc.finalizeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("control command did not reach Finalize")
		}
		select {
		case <-stdin.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("control command did not enter Stdin().Close()")
		}
		cancel()
		var err error
		select {
		case err = <-steerErrCh:
		case <-time.After(2 * time.Second):
			t.Fatal("steer did not return after cancel")
		}
		if !errors.Is(err, errControlCleanup) || !errors.Is(err, context.Canceled) {
			t.Fatalf("steer = %v, want errControlCleanup + Canceled", err)
		}
		awaitTurnEnded(t, fixture.session)
		first := awaitCloseOutcome(t, closeAsync(fixture.session), 2*time.Second)
		if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
			t.Fatalf("close while control stdin close pending = %#v, want ExecutionNone and ResourcesReleased=false", first)
		}
		stdin.release()
		s := fixture.session.(*session)
		var tracked processRelease
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			if len(s.releases) > 0 {
				tracked = s.releases[0]
			}
			return nil
		})
		if tracked == nil {
			t.Fatal("pending control stdin release was not tracked in s.releases")
		}
		select {
		case <-tracked.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("tracked control release did not complete after stdin closed")
		}
		again := awaitCloseOutcome(t, closeAsync(fixture.session), 2*time.Second)
		if again.ExecutionState != provider.ExecutionNone || !again.ResourcesReleased {
			t.Fatalf("close after control stdin closed = %#v, want ExecutionNone and ResourcesReleased=true", again)
		}
	})

	t.Run("wrapControlStdin preserves optional interfaces and signals directly", func(t *testing.T) {
		baseProc := newGatedRunProcess()
		wrappedBase, sigBase := wrapControlStdin(baseProc)
		if _, ok := wrappedBase.(processruntime.Finalizer); ok {
			t.Fatal("wrapControlStdin added Finalizer to a process without it")
		}
		if _, ok := wrappedBase.(processruntime.RootExitObserver); ok {
			t.Fatal("wrapControlStdin added RootExitObserver to a process without it")
		}
		_ = wrappedBase.Stdin().Close()
		if done, closeErr, waitErr := sigBase.await(context.Background()); !done || closeErr != nil || waitErr != nil {
			t.Fatalf("sigBase.await = %t, %v, %v", done, closeErr, waitErr)
		}

		finProc := &scriptedControlProcess{stdin: nopWriteCloser{Writer: io.Discard}}
		wrappedFin, _ := wrapControlStdin(finProc)
		if _, ok := wrappedFin.(processruntime.Finalizer); !ok {
			t.Fatal("wrapControlStdin dropped Finalizer")
		}
		if _, ok := wrappedFin.(processruntime.RootExitObserver); ok {
			t.Fatal("wrapControlStdin added RootExitObserver to a process without it")
		}

		fullProc := newCleanExitFinalizeProcess()
		wrappedFull, _ := wrapControlStdin(fullProc)
		if _, ok := wrappedFull.(processruntime.Finalizer); !ok {
			t.Fatal("wrapControlStdin dropped Finalizer on full process")
		}
		if _, ok := wrappedFull.(processruntime.RootExitObserver); !ok {
			t.Fatal("wrapControlStdin dropped RootExitObserver on full process")
		}
	})
}

// Open decision (Option A): a cancelled control command whose KillTree is
// refused (or lacks tree-kill support) must report errControlCleanup and keep
// Close's ResourcesReleased false even after the root and pipes exit cleanly.
func TestCancelledControlRefusedKillBlocksResourcesReleased(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*stdinAccountingProcess)
		wantErr   error
	}{
		{"refused kill", func(p *stdinAccountingProcess) { p.killErr = errFakeKill }, errFakeKill},
		{"tree kill unsupported", func(p *stdinAccountingProcess) { p.treeKillUnsupp = true }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdoutR, stdoutW := io.Pipe()
			proc := newStdinAccountingProcess(nopWriteCloser{Writer: io.Discard})
			proc.stdout = stdoutR
			proc.waitGate = make(chan struct{})
			tc.configure(proc)
			t.Cleanup(func() {
				_ = stdoutW.Close()
				proc.releaseWait()
			})
			// Release stdout and Wait once KillTree has been invoked so
			// abandon completes cleanly within StopGrace with the refused
			// kill / unsupported tree kill.
			go func() {
				<-proc.killEntered
				_ = stdoutW.Close()
				proc.releaseWait()
			}()
			control := cliPromptControl{
				fakeControl: &fakeControl{},
				cli:         &cliControl{executable: "opencode", launcher: oneRunLauncher{proc}, stopGrace: time.Second},
			}
			fixture := newReconcileFixture(t, control, 200*time.Millisecond, func(s *session) {
				s.config.StopGrace = time.Second
			})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			err := steerOnce(ctx, fixture)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errControlCleanup) {
				t.Fatalf("steer = %v, want DeadlineExceeded + errControlCleanup", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("steer = %v, want wrapped %v", err, tc.wantErr)
			}
			awaitTurnEnded(t, fixture.session)
			for i := range 2 {
				res := awaitCloseOutcome(t, closeAsync(fixture.session), 2*time.Second)
				if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
					t.Fatalf("close #%d after cancelled control (%s) = %#v, want ExecutionNone and ResourcesReleased=false", i+1, tc.name, res)
				}
			}
		})
	}
}
