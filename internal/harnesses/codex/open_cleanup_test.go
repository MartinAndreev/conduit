package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// silentAppServer reads requests but never answers, and ignores KillTree, so
// a failed open's cleanup stays pending until the test reaps it.
type silentAppServer struct {
	mu   sync.Mutex
	proc *silentProcess
}

type silentProcess struct {
	stdin        *io.PipeWriter
	stdout       *io.PipeReader
	stderr       *io.PipeReader
	serverOutput *io.PipeWriter
	stderrWriter *io.PipeWriter
	done         chan struct{}
	exitOnce     sync.Once
	kills        atomic.Int32
}

func (l *silentAppServer) Start(processruntime.Spec) (processruntime.Process, error) {
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	proc := &silentProcess{stdin: clientInput, stdout: clientOutput, stderr: stderrReader, serverOutput: serverOutput, stderrWriter: stderrWriter, done: make(chan struct{})}
	go func() { _, _ = io.Copy(io.Discard, serverInput) }()
	_ = lockscope.WithMutexLock(&l.mu, func() error { l.proc = proc; return nil })
	return proc, nil
}

func (l *silentAppServer) process() *silentProcess {
	var proc *silentProcess
	_ = lockscope.WithMutexLock(&l.mu, func() error { proc = l.proc; return nil })
	return proc
}

func (*silentProcess) PID() int                { return 42 }
func (p *silentProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *silentProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *silentProcess) Stderr() io.ReadCloser { return p.stderr }
func (*silentProcess) Interrupt() error        { return nil }
func (*silentProcess) TreeKillSupported() bool { return true }
func (p *silentProcess) KillTree() error       { p.kills.Add(1); return nil }
func (p *silentProcess) Wait() processruntime.Result {
	<-p.done
	return processruntime.Result{ExitedAt: time.Now()}
}

// exit reaps the process and closes its server-side pipes.
func (p *silentProcess) exit() {
	p.exitOnce.Do(func() {
		_ = p.serverOutput.Close()
		_ = p.stderrWriter.Close()
		close(p.done)
	})
}

func TestFailedOpenCleanupIsBoundedByCallerContext(t *testing.T) {
	launcher := &silentAppServer{}
	adapter := New(Config{Launcher: launcher, CloseGrace: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := adapter.OpenSession(ctx, provider.OpenSessionRequest{
		RequestID: "open", Assignment: testAssignment(1),
		Workspace:      provider.WorkspaceBinding{Path: t.TempDir(), Writable: true},
		Model:          provider.ModelSelection{ProviderID: "openai", ModelID: "model"},
		ResultContract: provider.ResultContractRef{Schema: json.RawMessage(`{"type":"object"}`), Requested: provider.SchemaNative},
		PermissionMode: provider.PermissionDeny,
	})
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("failed open waited %s past the caller deadline", elapsed)
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorDeadline {
		t.Fatalf("open error = %v, want deadline classification", err)
	}
	var pending *CleanupPendingError
	if !errors.As(err, &pending) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("open error lost the pending cleanup owner: %v", err)
	}
	proc := launcher.process()
	for deadline := time.Now().Add(2 * time.Second); proc.kills.Load() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not request group termination")
		}
	}
	select {
	case <-pending.Owner.Done():
		t.Fatal("cleanup completed before the process was reaped")
	default:
	}
	proc.exit()
	select {
	case <-pending.Owner.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("retained cleanup never completed")
	}
	if state := pending.Owner.Latest(); !state.Complete || !state.RootReaped || !state.ReadersDone {
		t.Fatalf("cleanup state = %+v", state)
	}
}
