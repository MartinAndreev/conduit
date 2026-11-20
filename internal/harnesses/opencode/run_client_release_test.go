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

// naturalFinalizeProcess exits cleanly right away and blocks inside Finalize
// after signalling finalizeEntered, so a test can interleave Close right
// after readRun reserved natural finalization and before readRun clears
// s.turn. Like internal/process, KillTree returns ErrSignalsFinalized once
// Finalize was entered.
type naturalFinalizeProcess struct {
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
	finalizeErr     error
}

func newNaturalFinalizeProcess() *naturalFinalizeProcess {
	root := make(chan struct{})
	close(root)
	return &naturalFinalizeProcess{
		rootDone:        root,
		finalizeEntered: make(chan struct{}),
		finalizeGate:    make(chan struct{}),
		finalizeDone:    make(chan struct{}),
	}
}

func (*naturalFinalizeProcess) PID() int                      { return 31 }
func (*naturalFinalizeProcess) Stdin() io.WriteCloser         { return nopWriteCloser{Writer: io.Discard} }
func (*naturalFinalizeProcess) Stdout() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (*naturalFinalizeProcess) Stderr() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (*naturalFinalizeProcess) Interrupt() error              { return nil }
func (*naturalFinalizeProcess) TreeKillSupported() bool       { return true }
func (p *naturalFinalizeProcess) RootExited() <-chan struct{} { return p.rootDone }
func (*naturalFinalizeProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: 0}
}
func (*naturalFinalizeProcess) Wait() processruntime.Result {
	return processruntime.Result{ExitCode: 0}
}
func (p *naturalFinalizeProcess) KillTree() error {
	p.kills.Add(1)
	if p.finalized.Load() {
		return processruntime.ErrSignalsFinalized
	}
	return nil
}
func (p *naturalFinalizeProcess) Finalize(ctx context.Context) error {
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
func (p *naturalFinalizeProcess) release() {
	p.gateOnce.Do(func() { close(p.finalizeGate) })
}

var (
	_ processruntime.Finalizer        = (*naturalFinalizeProcess)(nil)
	_ processruntime.RootExitObserver = (*naturalFinalizeProcess)(nil)
)

// finalizedKillProcess stays active on stdout (so readRun cannot reserve
// natural finalization) while KillTree returns ErrSignalsFinalized, as on
// fallback Unix platforms where the root was already reaped on exit.
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
	root := make(chan struct{})
	close(root)
	return &finalizedKillProcess{stdoutR: r, stdoutW: w, done: make(chan struct{}), rootDone: root}
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

// Limit 1 + Option A: when readRun reserves natural finalization just before
// Close claims the client, Close must not send a kill (which would get
// ErrSignalsFinalized) and must await that natural release instead. Because
// the process group was never killed, descendants without open pipes are not
// proven stopped, so Close returns ExecutionNone and ResourcesReleased=false.
// An actual ErrSignalsFinalized from KillTree is likewise never confirmed.
func TestCloseAwaitsNaturalRunFinalizationWithoutKill(t *testing.T) {
	t.Run("natural finalization finishes while Close waits without killing", func(t *testing.T) {
		proc := newNaturalFinalizeProcess()
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, time.Second)
		select {
		case <-proc.finalizeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("readRun did not enter natural finalization")
		}
		closed := closeAsync(sess)
		waitForState(t, sess, provider.SessionClosed)
		proc.release()
		outcome := <-closed
		if outcome.err != nil {
			t.Fatalf("close error = %v", outcome.err)
		}
		if outcome.res.ExecutionState != provider.ExecutionNone || outcome.res.ResourcesReleased {
			t.Fatalf("close after natural finalization = %#v, want ExecutionNone and ResourcesReleased=false", outcome.res)
		}
		select {
		case <-proc.finalizeDone:
		case <-time.After(2 * time.Second):
			t.Fatal("natural finalization did not complete")
		}
		if kills := proc.kills.Load(); kills != 0 {
			t.Fatalf("kills = %d, want 0 (Close must await natural finalization without killing)", kills)
		}
		if again := closeForAccounting(t, sess); again.ExecutionState != provider.ExecutionNone || again.ResourcesReleased || proc.kills.Load() != 0 {
			t.Fatalf("repeated close = %#v kills = %d", again, proc.kills.Load())
		}
	})

	t.Run("repeated Close after timeout awaits same natural release without killing", func(t *testing.T) {
		proc := newNaturalFinalizeProcess()
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, 40*time.Millisecond)
		select {
		case <-proc.finalizeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("readRun did not enter natural finalization")
		}
		first := closeForAccounting(t, sess)
		if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
			t.Fatalf("first close while natural finalization pending = %#v", first)
		}
		if kills := proc.kills.Load(); kills != 0 {
			t.Fatalf("kills = %d, want 0", kills)
		}
		proc.release()
		select {
		case <-proc.finalizeDone:
		case <-time.After(2 * time.Second):
			t.Fatal("natural finalization did not finish")
		}
		again := closeForAccounting(t, sess)
		if again.ExecutionState != provider.ExecutionNone || again.ResourcesReleased {
			t.Fatalf("second close after natural finalization finished = %#v, want ExecutionNone and ResourcesReleased=false", again)
		}
		if kills := proc.kills.Load(); kills != 0 {
			t.Fatalf("kills = %d, want 0", kills)
		}
	})

	t.Run("failed natural finalization stays unreleased", func(t *testing.T) {
		proc := newNaturalFinalizeProcess()
		proc.finalizeErr = errors.New("reap failed")
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, time.Second)
		select {
		case <-proc.finalizeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("readRun did not enter natural finalization")
		}
		closed := closeAsync(sess)
		waitForState(t, sess, provider.SessionClosed)
		proc.release()
		first := <-closed
		if first.err != nil {
			t.Fatalf("first close error = %v", first.err)
		}
		for i, res := range []provider.CloseSessionResult{first.res, closeForAccounting(t, sess)} {
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d after failed natural finalization = %#v", i+1, res)
			}
		}
		if kills := proc.kills.Load(); kills != 0 {
			t.Fatalf("kills = %d, want 0", kills)
		}
	})

	t.Run("ErrSignalsFinalized from KillTree is never confirmed", func(t *testing.T) {
		proc := newFinalizedKillProcess()
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, 50*time.Millisecond)
		for i := range 2 {
			res := closeForAccounting(t, sess)
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d with ErrSignalsFinalized = %#v, want ResourcesReleased=false", i+1, res)
			}
		}
		if kills := proc.kills.Load(); kills != 1 {
			t.Fatalf("kills = %d, want 1", kills)
		}
	})
}

// countingHeldPipeProcess models an escaped descendant holding stdout after
// the root exited cleanly: KillTree succeeds and Finalize succeeds, while
// Wait stays blocked until release(). waitEntered and waitExited synchronize
// deterministically without sleeps.
type countingHeldPipeProcess struct {
	stdoutR       *io.PipeReader
	stdoutW       *io.PipeWriter
	rootDone      chan struct{}
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
	root := make(chan struct{})
	close(root)
	return &countingHeldPipeProcess{
		stdoutR:     r,
		stdoutW:     w,
		rootDone:    root,
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
	return processruntime.Result{ExitCode: 0}
}
func (p *countingHeldPipeProcess) KillTree() error {
	p.kills.Add(1)
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
	return processruntime.Result{ExitCode: 0}
}
func (p *countingHeldPipeProcess) release() {
	p.releaseOnce.Do(func() {
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
		res := closeForAccounting(t, sess)
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
	final := closeForAccounting(t, sess)
	if final.ExecutionState != provider.ExecutionNone || !final.ResourcesReleased {
		t.Fatalf("close after pipe release = %#v, want ExecutionNone and ResourcesReleased=true", final)
	}
	if waits := proc.waits.Load(); waits != 1 || proc.kills.Load() != 1 {
		t.Fatalf("after recovery waits=%d kills=%d, want 1 and 1", waits, proc.kills.Load())
	}
}

// cleanExitedHeldPipeProcess models a run client whose root exited with code
// 0 while a descendant outside its group holds stderr. Because the root exit
// is clean, watchRunRootExit does not kill it; watchServer must kill it via
// the shared owner and call RequestFinalize right after, without waiting for
// the held pipe.
type cleanExitedHeldPipeProcess struct {
	stderrR        *io.PipeReader
	stderrW        *io.PipeWriter
	rootDone       chan struct{}
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

func newCleanExitedHeldPipeProcess(killErr error) *cleanExitedHeldPipeProcess {
	r, w := io.Pipe()
	root := make(chan struct{})
	close(root)
	return &cleanExitedHeldPipeProcess{
		stderrR:        r,
		stderrW:        w,
		rootDone:       root,
		waitGate:       make(chan struct{}),
		finalizeCalled: make(chan struct{}),
		killErr:        killErr,
	}
}

func (*cleanExitedHeldPipeProcess) PID() int                      { return 34 }
func (*cleanExitedHeldPipeProcess) Stdin() io.WriteCloser         { return nopWriteCloser{Writer: io.Discard} }
func (*cleanExitedHeldPipeProcess) Stdout() io.ReadCloser         { return io.NopCloser(strings.NewReader("")) }
func (p *cleanExitedHeldPipeProcess) Stderr() io.ReadCloser       { return p.stderrR }
func (*cleanExitedHeldPipeProcess) Interrupt() error              { return nil }
func (*cleanExitedHeldPipeProcess) TreeKillSupported() bool       { return true }
func (p *cleanExitedHeldPipeProcess) RootExited() <-chan struct{} { return p.rootDone }
func (*cleanExitedHeldPipeProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: 0}
}
func (p *cleanExitedHeldPipeProcess) KillTree() error {
	p.kills.Add(1)
	if p.finalized.Load() {
		return processruntime.ErrSignalsFinalized
	}
	return p.killErr
}
func (p *cleanExitedHeldPipeProcess) Finalize(context.Context) error {
	p.finalized.Store(true)
	p.finalizes.Add(1)
	p.finalizeOnce.Do(func() { close(p.finalizeCalled) })
	return nil
}
func (p *cleanExitedHeldPipeProcess) Wait() processruntime.Result {
	<-p.waitGate
	p.waited.Store(true)
	return processruntime.Result{ExitCode: 0}
}
func (p *cleanExitedHeldPipeProcess) release() {
	p.releaseOnce.Do(func() {
		_ = p.stderrW.Close()
		close(p.waitGate)
	})
}

var (
	_ processruntime.Finalizer        = (*cleanExitedHeldPipeProcess)(nil)
	_ processruntime.RootExitObserver = (*cleanExitedHeldPipeProcess)(nil)
)

// Limit 3: on server loss, watchServer terminates the active run client
// through the shared owner and registers finalization right after its kill,
// even when the root exited 0 and a descendant still holds a pipe. Later
// Close reuses that single kill's outcome without sending another kill.
func TestServerLossTerminatesAndFinalizesCleanExitedRunWithHeldPipe(t *testing.T) {
	for _, tc := range []struct {
		name         string
		killErr      error
		wantReleased bool
	}{
		{"confirmed server-loss kill", nil, true},
		{"refused server-loss kill", errors.New("kill denied"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc := newCleanExitedHeldPipeProcess(tc.killErr)
			t.Cleanup(proc.release)
			sess, srv := openSessionWithGrace(t, oneRunLauncher{proc}, &fakeServerLauncher{}, 50*time.Millisecond)
			if proc.kills.Load() != 0 || proc.finalized.Load() {
				t.Fatalf("clean root exit killed=%d finalized=%t before server loss", proc.kills.Load(), proc.finalized.Load())
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
			pending := closeForAccounting(t, sess)
			if pending.ExecutionState != provider.ExecutionNone || pending.ResourcesReleased {
				t.Fatalf("close while pipe held = %#v", pending)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("Close sent a second kill after watchServer: kills = %d", kills)
			}
			proc.release()
			done := closeForAccounting(t, sess)
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
// forced pipe close, and Wait can be independently scripted and gated.
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
	return processruntime.Result{}
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

// Finding 1: stdin close failure must retain kill outcome, stdin error, and
// forced pipe close errors in runClient even after Wait/Finalize finish and
// s.client clears; and Close must not observe a premature release while
// StartTurn's stdin close is still pending.
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
		// Wait until handleStartStdinCloseFailure clears s.client.
		s := opened.Session.(*session)
		select {
		case <-proc.waitDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Wait did not finish")
		}
		for i := range 2 {
			res := closeForAccounting(t, opened.Session)
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
			res := closeForAccounting(t, opened.Session)
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d after stdin close error = %#v, want ExecutionNone and ResourcesReleased=false", i+1, res)
			}
		}
	})

	t.Run("forced stdout close error after kill failure is retained in release facts", func(t *testing.T) {
		stdoutErr := errors.New("stdout close failed")
		proc := newStdinAccountingProcess(failingCloser{err: errors.New("stdin close failed")})
		proc.killErr = errors.New("kill denied")
		proc.stdout = closeErrReader{ReadCloser: io.NopCloser(strings.NewReader("")), closeErr: stdoutErr}
		adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: oneRunLauncher{proc}, StopGrace: time.Second}))
		req := openRequest()
		opened, err := adapter.OpenSession(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		_, err = opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
			Session: opened.Session.Ref(), Assignment: req.Assignment, ResultContract: req.ResultContract,
		})
		if !errors.Is(err, stdoutErr) {
			t.Fatalf("StartTurn err = %v, want joined stdout close error", err)
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
		select {
		case <-tracked.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("runClient release did not finish")
		}
		if facts := tracked.releaseFacts(); !facts.PipeCloseFailed || tracked.pipeCloseError() == nil {
			t.Fatalf("releaseFacts = %#v pipeCloseErr = %v, want PipeCloseFailed=true", facts, tracked.pipeCloseError())
		}
		if res := closeForAccounting(t, opened.Session); res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
			t.Fatalf("close = %#v, want ExecutionNone and ResourcesReleased=false", res)
		}
	})

	t.Run("Close waits for pending StartTurn stdin close before deciding release", func(t *testing.T) {
		stdin := newGatedCloser(errors.New("stdin close failed"))
		t.Cleanup(stdin.release)
		proc := newStdinAccountingProcess(stdin)
		adapter := New(withFakeServer(Config{Control: &fakeControl{}, Launcher: oneRunLauncher{proc}, StopGrace: time.Second}))
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
		closed := closeAsync(opened.Session)
		// KillTree and Wait both finish right away while Stdin().Close() is
		// still blocked; Close must wait for stdinDone instead of reporting
		// ResourcesReleased=true before StartTurn records the stdin error.
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
		select {
		case out := <-closed:
			t.Fatalf("Close returned %#v (err=%v) while stdin close was still pending", out.res, out.err)
		default:
		}
		stdin.release()
		if startErr := <-startDone; startErr == nil {
			t.Fatal("StartTurn succeeded despite stdin close error")
		}
		out := <-closed
		if out.err != nil {
			t.Fatalf("Close error = %v", out.err)
		}
		if out.res.ExecutionState != provider.ExecutionNone || out.res.ResourcesReleased {
			t.Fatalf("Close = %#v, want ExecutionNone and ResourcesReleased=false", out.res)
		}
	})
}

// Finding 2: a blocked KillTree (started by watchServer or Close itself) must
// not block Close beyond StopGrace or prevent Close from stopping the server.
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

	// Close starts the single kill worker, hits StopGrace while KillTree is
	// blocked, and must still proceed to stop the session's own server.
	started := time.Now()
	first := closeForAccounting(t, sess)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Close was blocked by KillTree for %s", elapsed)
	}
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

	// Unblock KillTree and Wait: the shared worker and release observer
	// finish, and a repeated Close confirms release with only one KillTree.
	proc.releaseKill()
	proc.releaseWait()
	select {
	case <-client.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("shared release observer did not complete after KillTree unblocked")
	}
	second := closeForAccounting(t, sess)
	if second.ExecutionState != provider.ExecutionNone || !second.ResourcesReleased {
		t.Fatalf("second Close after KillTree unblocked = %#v, want ExecutionNone and ResourcesReleased=true", second)
	}
	if kills := proc.kills.Load(); kills != 1 {
		t.Fatalf("kills = %d, want 1", kills)
	}
}

// Finding 3: even when KillTree is refused or tree kill is unsupported,
// Close must still start the shared release observer so releaseDone closes
// once the process eventually exits, while DecideClientCloseRelease keeps
// ResourcesReleased false.
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
			first := closeForAccounting(t, sess)
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
			second := closeForAccounting(t, sess)
			if second.ExecutionState != provider.ExecutionNone || second.ResourcesReleased {
				t.Fatalf("second close = %#v, want ExecutionNone and ResourcesReleased=false", second)
			}
		})
	}
}

// Finding 4: if server cleanup finishes right after Await timed out and
// before abortServer checks cleanup.Done, abortServer must recompute both
// stop and release decisions from cleanup.Latest() so ReleaseErr and pipe
// close errors are never lost (and a clean finish is not misreported).
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
		server.afterStopAwait = func() {
			close(waitGate)
			<-server.cleanup.Done()
		}
		cause := errors.New("create session failed")
		got := abortServer(context.Background(), server, cause)
		var pending *CleanupPendingError
		if errors.As(got, &pending) {
			t.Fatalf("abortServer returned CleanupPendingError although Done was closed: %v", got)
		}
		if !errors.Is(got, cause) || !errors.Is(got, waitReleaseErr) || !errors.Is(got, stdoutCloseErr) {
			t.Fatalf("abortServer = %v, want cause + waitReleaseErr + stdoutCloseErr", got)
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
		server.afterStopAwait = func() {
			close(waitGate)
			<-server.cleanup.Done()
		}
		cause := errors.New("create session failed")
		got := abortServer(context.Background(), server, cause)
		if !errors.Is(got, cause) {
			t.Fatalf("abortServer = %v, want cause", got)
		}
		var pending *CleanupPendingError
		if errors.As(got, &pending) {
			t.Fatalf("abortServer returned CleanupPendingError on clean final snapshot: %v", got)
		}
		if got != cause {
			t.Fatalf("abortServer = %v, want unmodified cause %v", got, cause)
		}
	})
}

type scriptedControlProcess struct {
	stdin  io.WriteCloser
	output string
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
func (*scriptedControlProcess) Finalize(context.Context) error { return nil }

var _ processruntime.Finalizer = (*scriptedControlProcess)(nil)

// Finding 5: the normal completion path of cliControl.run must include the
// cleanup owner's stdin close outcome in release confirmation and hand any
// unconfirmed release to endControlOp so Close accounts for it.
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
			res := closeForAccounting(t, fixture.session)
			if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
				t.Fatalf("close #%d after failed control stdin close = %#v, want ExecutionNone and ResourcesReleased=false", i+1, res)
			}
		}
	})

	t.Run("pending stdin close stays tracked until stdin closes cleanly", func(t *testing.T) {
		stdin := newGatedCloser(nil)
		t.Cleanup(stdin.release)
		proc := &scriptedControlProcess{
			stdin:  stdin,
			output: `{"data":{"id":"receipt"}}`,
		}
		control := cliPromptControl{
			fakeControl: &fakeControl{},
			cli:         &cliControl{executable: "opencode", launcher: oneRunLauncher{proc}},
		}
		fixture := newReconcileFixture(t, control, 300*time.Millisecond, func(s *session) {
			s.config.StopGrace = 60 * time.Millisecond
		})
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		err := steerOnce(ctx, fixture)
		cancel()
		if !errors.Is(err, errControlCleanup) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("steer = %v, want errControlCleanup + DeadlineExceeded", err)
		}
		select {
		case <-stdin.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("control command did not attempt stdin close")
		}
		awaitTurnEnded(t, fixture.session)
		first := closeForAccounting(t, fixture.session)
		if first.ExecutionState != provider.ExecutionNone || first.ResourcesReleased {
			t.Fatalf("close while control stdin close pending = %#v, want ExecutionNone and ResourcesReleased=false", first)
		}
		stdin.release()
		// Wait for the tracked finishedRelease to observe stdin completion.
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
		again := closeForAccounting(t, fixture.session)
		if again.ExecutionState != provider.ExecutionNone || !again.ResourcesReleased {
			t.Fatalf("close after control stdin closed = %#v, want ExecutionNone and ResourcesReleased=true", again)
		}
	})
}
