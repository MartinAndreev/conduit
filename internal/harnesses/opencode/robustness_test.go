package opencode

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// killableProcess keeps the run client alive until KillTree is called.
type killableProcess struct {
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	killed       chan struct{}
	once         sync.Once
}

func newKillableProcess() *killableProcess {
	reader, writer := io.Pipe()
	return &killableProcess{stdoutReader: reader, stdoutWriter: writer, killed: make(chan struct{})}
}
func (*killableProcess) PID() int                { return 7 }
func (*killableProcess) Stdin() io.WriteCloser   { return nopWriteCloser{Writer: io.Discard} }
func (p *killableProcess) Stdout() io.ReadCloser { return p.stdoutReader }
func (*killableProcess) Stderr() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (*killableProcess) Interrupt() error        { return nil }
func (*killableProcess) TreeKillSupported() bool { return true }
func (p *killableProcess) Wait() processruntime.Result {
	<-p.killed
	return processruntime.Result{ExitCode: -1, Signal: "killed"}
}
func (p *killableProcess) KillTree() error {
	select {
	case <-p.killed:
		return os.ErrProcessDone
	default:
	}
	p.once.Do(func() { close(p.killed); _ = p.stdoutWriter.Close() })
	return nil
}

type killableLauncher struct{ proc *killableProcess }

func (l *killableLauncher) Start(processruntime.Spec) (processruntime.Process, error) {
	return l.proc, nil
}

func openActiveSession(t *testing.T, launcher processruntime.Launcher) provider.Session {
	t.Helper()
	adapter := New(Config{Control: &fakeControl{}, Launcher: launcher, StopGrace: time.Second})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}, ResultContract: c, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c}); err != nil {
		t.Fatal(err)
	}
	return opened.Session
}

func TestInterruptThenCloseStopsOwnedRunClient(t *testing.T) {
	proc := newKillableProcess()
	session := openActiveSession(t, &killableLauncher{proc: proc})
	result, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseInterruptThenClose})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.killed:
	default:
		t.Fatal("owned run client was not terminated")
	}
	if !result.HandleClosed || !result.ResourcesReleased || result.ExecutionState != provider.ExecutionUnknown {
		t.Fatalf("close result = %#v; native execution must stay unknown", result)
	}
	snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
	if snapshot.SessionState != provider.SessionClosed || snapshot.ActiveTurn.Present {
		t.Fatalf("snapshot after stop = %#v", snapshot)
	}
}

func TestRequireQuiescentStillRefusesActiveTurn(t *testing.T) {
	proc := newKillableProcess()
	defer proc.KillTree()
	session := openActiveSession(t, &killableLauncher{proc: proc})
	if _, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseRequireQuiescent}); err == nil {
		t.Fatal("require_quiescent closed an active turn")
	}
	select {
	case <-proc.killed:
		t.Fatal("refused close terminated the run client")
	default:
	}
}

func TestCloseStopRule(t *testing.T) {
	d := rules.DecideClose(rules.CloseFacts{SessionMatches: true, ActiveTurn: true, InterruptThenClose: true})
	if d.Kind != rules.CloseStopOwnedClient || d.Reason == "" {
		t.Fatalf("decision = %#v", d)
	}
	if d := rules.DecideClose(rules.CloseFacts{SessionMatches: true, InterruptThenClose: true}); d.Kind != rules.CloseAllowed {
		t.Fatalf("idle interrupt_then_close = %#v", d)
	}
}

// One tool event above the text bound but within the line bound is normalized
// with truncated fields instead of losing the session.
func TestLargeToolLineIsTruncatedNotLost(t *testing.T) {
	output := strings.Repeat("y", 200*1024)
	line := `{"type":"tool_use","sessionID":"native-session","part":{"id":"tool","tool":"read","state":{"status":"completed","input":{"path":"big.txt"},"output":"` + output + `"}}}` + "\n"
	launcher := &fakeLauncher{output: line}
	session := openActiveSession(t, launcher)
	stream, _ := session.Events(context.Background(), provider.EventCursor{})
	time.Sleep(150 * time.Millisecond)
	snapshot, _ := session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: session.Ref()})
	if snapshot.SessionState == provider.SessionLost {
		t.Fatal("large JSONL line lost the session")
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatal("tool completion was not observed")
		}
		if completed, ok := event.Body().(provider.ToolCompletedEvent); ok {
			if !completed.Output.Truncated {
				t.Fatal("tool output was not truncated")
			}
			return
		}
	}
}

func TestUnresolvedSteerLeavesEnvelopeAttributionAbsent(t *testing.T) {
	s := &session{config: withDefaults(Config{NewID: func(prefix string) (string, error) { return prefix, nil }}), ref: provider.SessionRef{AdapterSessionID: "s"}, nativeSession: "n", assignment: provider.AssignmentRef{Revision: 1}, broker: eventstream.New(8)}
	stream, _ := s.broker.Subscribe()
	s.emit(provider.SessionStateEvent{})
	s.deliveryUnknown = true
	s.emit(provider.SessionStateEvent{})
	resolved, _ := stream.Receive(context.Background())
	unresolved, _ := stream.Receive(context.Background())
	if !resolved.Envelope.Assignment.Present || unresolved.Envelope.Assignment.Present {
		t.Fatalf("attribution resolved=%v unresolved=%v", resolved.Envelope.Assignment.Present, unresolved.Envelope.Assignment.Present)
	}
}
