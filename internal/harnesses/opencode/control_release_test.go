package opencode

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// A finished control command whose release fails is not reported as
// success: the error wraps errControlCleanup and the cause.
func TestControlCommandReleaseFailureFailsCommand(t *testing.T) {
	reapErr := errors.New("reap failed")
	launcher := &fakeLauncher{output: "{}", delay: time.Millisecond, finalizeErr: reapErr}
	control := &cliControl{executable: "opencode", launcher: launcher}
	_, sent, err := control.run(context.Background(), "/work", "session", "list")
	if !sent || !errors.Is(err, errControlCleanup) || !errors.Is(err, reapErr) {
		t.Fatalf("run = sent %v, %v; want the release failure", sent, err)
	}
}

// A pending release is waited for only within the caller's context (and the
// cleanup budget); the command then fails with errControlCleanup and the
// context error, while the process keeps ownership of the release.
func TestControlCommandReleaseIsBoundedByCallerContext(t *testing.T) {
	launcher := &fakeLauncher{output: "{}", delay: time.Millisecond, finalizeBlock: make(chan struct{})}
	control := &cliControl{executable: "opencode", launcher: launcher}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, sent, err := control.run(ctx, "/work", "session", "list")
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("pending release blocked the command for %s", elapsed)
	}
	if !sent || !errors.Is(err, errControlCleanup) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run = sent %v, %v; want an unconfirmed release", sent, err)
	}
}

// A cancelled command's kill is its last signal: finalization is registered
// at once, without waiting for the command's pipes.
func TestControlCommandCancellationRegistersFinalization(t *testing.T) {
	launcher := &fakeLauncher{output: "{}", delay: 2 * time.Second}
	control := &cliControl{executable: "opencode", launcher: launcher}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, sent, err := control.run(ctx, "/work", "session", "list")
	if !sent || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run = sent %v, %v; want cancellation", sent, err)
	}
	if kills, calls := launcher.last.kills.Load(), launcher.last.finalizes.Load(); kills != 1 || calls != 1 {
		t.Fatalf("kills = %d, Finalize calls = %d, want one each", kills, calls)
	}
}

// A run whose release fails still publishes its final events; the failed
// release is reported as a protocol warning.
func TestRunReleaseFailureIsReportedWithoutBlockingFinalEvents(t *testing.T) {
	launcher := &fakeLauncher{delay: time.Millisecond, finalizeErr: errors.New("reap failed")}
	session := openActiveSession(t, launcher)
	stream, err := session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	warned := false
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v (release warning seen: %v)", err, warned)
		}
		switch body := event.Body().(type) {
		case provider.ProtocolWarningEvent:
			if body.Code == "opencode_run_release" {
				warned = true
			}
		case provider.SessionStateEvent:
			if body.Current == provider.SessionIdleObserved {
				if !warned {
					t.Fatal("final state published without the release warning")
				}
				return
			}
		}
	}
}

// heldPipeControlProcess is a control command whose pipes survive the group
// kill (a descendant outside the group keeps them): like production, Wait
// returns only once the parent's output ends were closed.
type heldPipeControlProcess struct {
	stdin, stdout, stderr *closeRecorder
	kills                 atomic.Int32
	finalizes             atomic.Int32
}

type closeRecorder struct {
	closed chan struct{}
	once   sync.Once
}

func newCloseRecorder() *closeRecorder { return &closeRecorder{closed: make(chan struct{})} }
func (r *closeRecorder) Read([]byte) (int, error) {
	<-r.closed
	return 0, io.EOF
}
func (r *closeRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *closeRecorder) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func (*heldPipeControlProcess) PID() int                { return 11 }
func (p *heldPipeControlProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *heldPipeControlProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *heldPipeControlProcess) Stderr() io.ReadCloser { return p.stderr }
func (*heldPipeControlProcess) Interrupt() error        { return nil }
func (*heldPipeControlProcess) TreeKillSupported() bool { return true }
func (p *heldPipeControlProcess) KillTree() error       { p.kills.Add(1); return nil }
func (p *heldPipeControlProcess) Finalize(context.Context) error {
	p.finalizes.Add(1)
	return nil
}
func (p *heldPipeControlProcess) Wait() processruntime.Result {
	<-p.stdout.closed
	<-p.stderr.closed
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}

type heldPipeControlLauncher struct{ proc *heldPipeControlProcess }

func (l heldPipeControlLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	return l.proc, nil
}

// Cancellation closes the parent's pipe ends independently of Wait, so the
// command's copy workers and Wait end although a descendant outside the
// group keeps its ends after the kill.
func TestControlCommandCancellationClosesPipesHeldByEscapedDescendant(t *testing.T) {
	proc := &heldPipeControlProcess{stdin: newCloseRecorder(), stdout: newCloseRecorder(), stderr: newCloseRecorder()}
	control := &cliControl{executable: "opencode", launcher: heldPipeControlLauncher{proc}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, sent, err := control.run(ctx, "/work", "session", "list")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled command returned after %s", elapsed)
	}
	if !sent || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run = sent %v, %v; want cancellation", sent, err)
	}
	if proc.kills.Load() != 1 || proc.finalizes.Load() != 1 {
		t.Fatalf("kills = %d, Finalize calls = %d, want one each", proc.kills.Load(), proc.finalizes.Load())
	}
	for name, pipe := range map[string]*closeRecorder{"stdin": proc.stdin, "stdout": proc.stdout, "stderr": proc.stderr} {
		select {
		case <-pipe.closed:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s was not closed after cancellation", name)
		}
	}
	waited := make(chan struct{})
	go func() { proc.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not end after the pipes were closed")
	}
}
