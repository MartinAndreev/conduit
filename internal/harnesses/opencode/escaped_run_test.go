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

// escapedRunProcess is a run client whose root already exited unsuccessfully
// while a descendant outside its process group keeps stderr: the group kill
// cannot end stderr, so Wait stays blocked until the test releases it. Like
// production, finalization refuses every later signal once registered.
type escapedRunProcess struct {
	stderr      *io.PipeReader
	stderrW     *io.PipeWriter
	rootDone    chan struct{}
	waitGate    chan struct{}
	releaseOnce sync.Once
	waited      atomic.Bool
	killErr     error
	kills       atomic.Int32
	registered  atomic.Bool
}

func newEscapedRunProcess(killErr error) *escapedRunProcess {
	reader, writer := io.Pipe()
	root := make(chan struct{})
	close(root)
	return &escapedRunProcess{stderr: reader, stderrW: writer, rootDone: root, waitGate: make(chan struct{}), killErr: killErr}
}

func (*escapedRunProcess) PID() int                { return 9 }
func (*escapedRunProcess) Stdin() io.WriteCloser   { return nopWriteCloser{Writer: io.Discard} }
func (*escapedRunProcess) Stdout() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (p *escapedRunProcess) Stderr() io.ReadCloser { return p.stderr }
func (*escapedRunProcess) Interrupt() error        { return nil }
func (*escapedRunProcess) TreeKillSupported() bool { return true }
func (p *escapedRunProcess) RootExited() <-chan struct{} {
	return p.rootDone
}
func (p *escapedRunProcess) RootResult() processruntime.Result {
	return processruntime.Result{ExitCode: 1, Err: errors.New("exit status 1")}
}
func (p *escapedRunProcess) Wait() processruntime.Result {
	<-p.waitGate
	p.waited.Store(true)
	return processruntime.Result{ExitCode: 1}
}
func (p *escapedRunProcess) KillTree() error {
	p.kills.Add(1)
	if p.registered.Load() {
		return processruntime.ErrSignalsFinalized
	}
	return p.killErr
}
func (p *escapedRunProcess) Finalize(context.Context) error {
	p.registered.Store(true)
	return nil
}

// release ends the escaped descendant: stderr and Wait complete.
func (p *escapedRunProcess) release() {
	p.releaseOnce.Do(func() {
		_ = p.stderrW.Close()
		close(p.waitGate)
	})
}

type oneRunLauncher struct{ proc processruntime.Process }

func (l oneRunLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	return l.proc, nil
}

func awaitRegistered(t *testing.T, proc *escapedRunProcess) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !proc.registered.Load() {
		if time.Now().After(deadline) {
			t.Fatal("failed run did not register finalization")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A failed run's kill is its last signal: finalization is registered right
// after it, while Wait is still blocked by the escaped descendant.
func TestFailedRunFinalizesWithoutWaitingForPipes(t *testing.T) {
	proc := newEscapedRunProcess(nil)
	t.Cleanup(proc.release)
	_ = openActiveSession(t, oneRunLauncher{proc})
	awaitRegistered(t, proc)
	if proc.kills.Load() == 0 {
		t.Fatal("finalization registered before the run's kill")
	}
	if proc.waited.Load() {
		t.Fatal("Wait returned although the escaped descendant holds stderr")
	}
}

// The failed run's kill is refused by the OS; finalization then refuses
// Close's kill. That refusal is no termination evidence: Close reports the
// resources unreleased.
func TestFailedRunKillIsNotConfirmedByFinalization(t *testing.T) {
	proc := newEscapedRunProcess(errors.New("kill denied"))
	t.Cleanup(proc.release)
	session := openActiveSession(t, oneRunLauncher{proc})
	awaitRegistered(t, proc)
	result, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseInterruptThenClose})
	if err != nil || result.ResourcesReleased {
		t.Fatalf("close = %#v, %v; want unreleased resources", result, err)
	}
}
