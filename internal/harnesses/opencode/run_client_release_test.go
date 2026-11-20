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

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// naturalFinalizeProcess exits cleanly right away and blocks inside Finalize
// after signalling finalizeEntered, so a test can interleave Close right
// after readRun reserved natural finalization and before readRun clears
// s.turn. Like internal/process, KillTree returns ErrSignalsFinalized once
// Finalize was entered.
type naturalFinalizeProcess struct {
	rootDone        chan struct{}
	finalizeEntered chan struct{}
	finalizeGate    chan struct{}
	enterOnce       sync.Once
	gateOnce        sync.Once
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

// Limit 1: when readRun reserves natural finalization just before Close
// claims the client, Close must not send a kill (which would get
// ErrSignalsFinalized) and must await that natural release instead.
// Conversely, an actual ErrSignalsFinalized from KillTree is never a
// confirmed kill.
func TestCloseAwaitsNaturalRunFinalizationWithoutKill(t *testing.T) {
	t.Run("natural finalization confirms while Close waits", func(t *testing.T) {
		proc := newNaturalFinalizeProcess()
		t.Cleanup(proc.release)
		sess, _ := openSessionWithGrace(t, oneRunLauncher{proc}, nil, time.Second)
		select {
		case <-proc.finalizeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("readRun did not enter natural finalization")
		}
		closed := make(chan provider.CloseSessionResult, 1)
		go func() {
			closed <- closeForAccounting(t, sess)
		}()
		waitForState(t, sess, provider.SessionClosed)
		proc.release()
		result := <-closed
		if result.ExecutionState != provider.ExecutionNone || !result.ResourcesReleased {
			t.Fatalf("close after natural finalization = %#v, want ExecutionNone and ResourcesReleased=true", result)
		}
		if kills := proc.kills.Load(); kills != 0 {
			t.Fatalf("kills = %d, want 0 (Close must await natural finalization without killing)", kills)
		}
		if again := closeForAccounting(t, sess); again.ExecutionState != provider.ExecutionNone || !again.ResourcesReleased || proc.kills.Load() != 0 {
			t.Fatalf("repeated close = %#v kills = %d", again, proc.kills.Load())
		}
	})

	t.Run("repeated Close after timeout confirms once natural release finishes", func(t *testing.T) {
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
		again := closeForAccounting(t, sess)
		if again.ExecutionState != provider.ExecutionNone || !again.ResourcesReleased {
			t.Fatalf("second close after natural finalization finished = %#v", again)
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
		closed := make(chan provider.CloseSessionResult, 1)
		go func() {
			closed <- closeForAccounting(t, sess)
		}()
		waitForState(t, sess, provider.SessionClosed)
		proc.release()
		for i, res := range []provider.CloseSessionResult{<-closed, closeForAccounting(t, sess)} {
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
// Wait stays blocked until release(). waits and inFlightWaits track whether
// repeated Close calls accumulate duplicate Wait goroutines.
type countingHeldPipeProcess struct {
	stdoutR       *io.PipeReader
	stdoutW       *io.PipeWriter
	rootDone      chan struct{}
	waitGate      chan struct{}
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
		stdoutR:  r,
		stdoutW:  w,
		rootDone: root,
		waitGate: make(chan struct{}),
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
	defer p.inFlightWaits.Add(-1)
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
	for i := range 5 {
		res := closeForAccounting(t, sess)
		if res.ExecutionState != provider.ExecutionNone || res.ResourcesReleased {
			t.Fatalf("close timeout #%d = %#v, want ExecutionNone and ResourcesReleased=false", i+1, res)
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
	deadline := time.Now().Add(2 * time.Second)
	for proc.inFlightWaits.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("shared Wait observer did not finish after pipe release")
		}
		time.Sleep(5 * time.Millisecond)
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
			// While the pipe is held, Close reuses watchServer's kill and
			// reports unreleased resources without sending another kill.
			pending := closeForAccounting(t, sess)
			if pending.ExecutionState != provider.ExecutionNone || pending.ResourcesReleased {
				t.Fatalf("close while pipe held = %#v", pending)
			}
			if kills := proc.kills.Load(); kills != 1 {
				t.Fatalf("Close sent a second kill after watchServer: kills = %d", kills)
			}
			// Once the descendant drops stderr, Close reports the outcome of
			// watchServer's single kill and shared release.
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
		cause     error
	}{
		{"Wait ReleaseErr", func(p *failingReleaseServerProcess) { p.waitReleaseErr = errors.New("job object close failed") }, errors.New("job object close failed")},
		{"stdout PipeCloseErr", func(p *failingReleaseServerProcess) { p.stdoutCloseErr = errors.New("stdout close failed") }, errors.New("stdout close failed")},
		{"stderr PipeCloseErr", func(p *failingReleaseServerProcess) { p.stderrCloseErr = errors.New("stderr close failed") }, errors.New("stderr close failed")},
		{"StdinCloseErr", func(p *failingReleaseServerProcess) { p.stdinCloseErr = errors.New("stdin close failed") }, errors.New("stdin close failed")},
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
