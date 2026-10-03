package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// The subprocess models run's non-TTY stdin gate, without OpenCode or a model.
func TestRunStdinEOFHelper(t *testing.T) {
	if os.Getenv("CONDUIT_OPENCODE_EOF_HELPER") != "1" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, `{"type":"text","sessionID":"native-session","part":{"id":"eof","text":"stdin reached EOF"}}`)
	os.Exit(0)
}

type eofLauncher struct {
	proc     *trackedEOFProcess
	starts   int
	closeErr error
	killHook func()
}

func (l *eofLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	l.starts++
	if len(spec.Args) == 0 || spec.Args[0] != "run" || spec.Args[len(spec.Args)-1] != "fixture prompt" {
		return nil, errors.New("prompt missing from run arguments")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	proc, err := (processruntime.ExecLauncher{}).Start(processruntime.Spec{Path: exe, Args: []string{"-test.run=^TestRunStdinEOFHelper$"}, Dir: spec.Dir, Env: []string{"CONDUIT_OPENCODE_EOF_HELPER=1"}})
	if err != nil {
		return nil, err
	}
	l.proc = &trackedEOFProcess{Process: proc, closeErr: l.closeErr, killHook: l.killHook, reaped: make(chan struct{})}
	return l.proc, nil
}

type trackedEOFProcess struct {
	processruntime.Process
	closeErr error
	killHook func()
	kills    atomic.Int32
	waitOnce atomic.Bool
	reaped   chan struct{}
}

func (p *trackedEOFProcess) Stdin() io.WriteCloser {
	return eofCloseWriter{WriteCloser: p.Process.Stdin(), err: p.closeErr}
}
func (p *trackedEOFProcess) KillTree() error {
	p.kills.Add(1)
	if p.killHook != nil {
		p.killHook()
	}
	return p.Process.KillTree()
}
func (p *trackedEOFProcess) Wait() processruntime.Result {
	result := p.Process.Wait()
	if p.waitOnce.CompareAndSwap(false, true) {
		close(p.reaped)
	}
	return result
}

type eofCloseWriter struct {
	io.WriteCloser
	err error
}

func (w eofCloseWriter) Close() error { return errors.Join(w.WriteCloser.Close(), w.err) }

func openEOFSession(t *testing.T, launcher processruntime.Launcher) (provider.Session, provider.StartTurnRequest) {
	t.Helper()
	assignment := provider.AssignmentRef{AssignmentID: "fixture", Revision: 1}
	contract := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, err := New(Config{Control: &fakeControl{}, Launcher: launcher}).OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: assignment, Workspace: provider.WorkspaceBinding{Path: t.TempDir(), Writable: true}, Model: provider.ModelSelection{ProviderID: "fixture", ModelID: "fixture"}, ResultContract: contract, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	return opened.Session, provider.StartTurnRequest{RequestID: "eof-start", Session: opened.Session.Ref(), Assignment: assignment, ResultContract: contract, Input: []provider.InputPart{{Text: "fixture prompt"}}}
}

func TestStartTurnClosesStdinBeforeRunJSONL(t *testing.T) {
	launcher := &eofLauncher{}
	session, request := openEOFSession(t, launcher)
	stream, err := session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := session.StartTurn(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	// Also cleans up the intentionally stuck old implementation on regression failure.
	defer func() { _ = launcher.proc.KillTree(); <-launcher.proc.reaped }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatalf("no JSONL after stdin EOF: %v", err)
		}
		if text, ok := event.Body().(provider.AssistantTextEvent); ok {
			if text.Update.Text != "stdin reached EOF" {
				t.Fatalf("unexpected text: %#v", text)
			}
			break
		}
	}
	select {
	case <-launcher.proc.reaped:
	case <-ctx.Done():
		t.Fatal("CLI was not reaped after EOF")
	}
}

func TestStartTurnStdinCloseFailureIsUncertainAndReapsWithoutRetry(t *testing.T) {
	cause := errors.New("stdin close failed after EOF")
	launcher := &eofLauncher{closeErr: cause}
	session, request := openEOFSession(t, launcher)
	launcher.killHook = func() {
		// ObserveState takes the mutex: cleanup must execute outside it.
		snapshot, err := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
		if err != nil || snapshot.Knowledge != provider.SnapshotUnknown {
			t.Errorf("failure not uncertain before cleanup: %#v %v", snapshot, err)
		}
	}
	result, err := session.StartTurn(context.Background(), request)
	var pe *provider.ProviderError
	if !errors.As(err, &pe) || !errors.Is(err, cause) || pe.Code != provider.ErrorDeliveryUnknown || pe.RemoteEffect != provider.RemoteEffectPossiblySent || pe.Retry != provider.RetryAfterReconcile || pe.RequestID.Value != request.RequestID || result.Turn.AdapterTurnID != "" {
		t.Fatalf("close error = %#v, result = %#v", err, result)
	}
	select {
	case <-launcher.proc.reaped:
	default:
		t.Fatal("failed start returned before owned CLI was reaped")
	}
	if launcher.proc.kills.Load() != 1 {
		t.Fatal("owned CLI not cleaned up once")
	}
	if _, err := session.StartTurn(context.Background(), request); err == nil || launcher.starts != 1 {
		t.Fatalf("uncertain start retried: launches=%d err=%v", launcher.starts, err)
	}
}

// A failed OS termination must neither hang StartTurn nor discard ownership.
type stuckCleanupLauncher struct {
	proc   *stuckCleanupProcess
	starts int
}

func (l *stuckCleanupLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	l.starts++
	return l.proc, nil
}

type stuckCleanupProcess struct {
	stdout, stderr    *io.PipeReader
	release           chan struct{}
	closeErr, killErr error
}

func (*stuckCleanupProcess) PID() int { return 1 }
func (p *stuckCleanupProcess) Stdin() io.WriteCloser {
	return eofCloseWriter{WriteCloser: nopWriteCloser{Writer: io.Discard}, err: p.closeErr}
}
func (p *stuckCleanupProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *stuckCleanupProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *stuckCleanupProcess) KillTree() error       { return p.killErr }
func (*stuckCleanupProcess) Interrupt() error        { return nil }
func (*stuckCleanupProcess) TreeKillSupported() bool { return true }
func (p *stuckCleanupProcess) Wait() processruntime.Result {
	<-p.release
	return processruntime.Result{}
}

func TestStdinCloseCleanupFailureIsBoundedAndRetainsOwnership(t *testing.T) {
	stdout, stdoutWriter := io.Pipe()
	stderr, stderrWriter := io.Pipe()
	defer stdoutWriter.Close()
	defer stderrWriter.Close()
	closeErr, killErr := errors.New("cannot close stdin"), errors.New("cannot kill owned CLI")
	proc := &stuckCleanupProcess{stdout: stdout, stderr: stderr, release: make(chan struct{}), closeErr: closeErr, killErr: killErr}
	launcher := &stuckCleanupLauncher{proc: proc}
	opened, request := openEOFSession(t, launcher)
	s := opened.(*session)
	defer func() {
		close(proc.release)
		deadline := time.Now().Add(time.Second)
		for {
			s.mu.Lock()
			reaped := s.process == nil
			s.mu.Unlock()
			if reaped {
				return
			}
			if time.Now().After(deadline) {
				t.Error("background cleanup did not release ownership after reap")
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	started := time.Now()
	_, err := s.StartTurn(context.Background(), request)
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("cleanup exceeded bound: %s", elapsed)
	}
	var pe *provider.ProviderError
	if !errors.As(err, &pe) || pe.Code != provider.ErrorDeliveryUnknown || pe.RemoteEffect != provider.RemoteEffectPossiblySent || !errors.Is(err, closeErr) || !errors.Is(err, killErr) || !strings.Contains(pe.Cause.Error(), "cleanup timed out") {
		t.Fatalf("cleanup causes lost: %v", err)
	}
	s.mu.Lock()
	retained := s.process == proc && s.turn.Present && s.deliveryUnknown && s.state == provider.SessionLost
	s.mu.Unlock()
	if !retained {
		t.Fatal("unreaped owned process or uncertainty discarded")
	}
	if _, err := s.StartTurn(context.Background(), request); err == nil || launcher.starts != 1 {
		t.Fatal("failed cleanup allowed retry")
	}
}
