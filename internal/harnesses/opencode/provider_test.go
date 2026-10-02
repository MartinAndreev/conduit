package opencode

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type fakeControl struct {
	create      CreateSessionRequest
	prompt      PromptRequest
	promptSent  bool
	promptErr   error
	promptCalls int
}

func (f *fakeControl) CreateSession(_ context.Context, request CreateSessionRequest) (CreateSessionResult, bool, error) {
	f.create = request
	return CreateSessionResult{SessionID: "native-session", Model: request.Model, ModelVerified: true}, true, nil
}
func (f *fakeControl) Prompt(_ context.Context, request PromptRequest) (PromptResult, bool, error) {
	f.promptCalls++
	f.prompt = request
	if f.promptErr != nil {
		return PromptResult{}, f.promptSent, f.promptErr
	}
	return PromptResult{Accepted: true, ReceiptID: "message-1"}, true, nil
}

type fakeLauncher struct {
	spec   processruntime.Spec
	output string
	starts int
	result processruntime.Result
	delay  time.Duration
}

func (f *fakeLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	f.spec = spec
	f.starts++
	delay := f.delay
	if delay == 0 {
		delay = 50 * time.Millisecond
	}
	return &fakeProcess{stdout: io.NopCloser(strings.NewReader(f.output)), stderr: io.NopCloser(strings.NewReader("")), result: f.result, delay: delay}, nil
}

type fakeProcess struct {
	stdout, stderr io.ReadCloser
	result         processruntime.Result
	delay          time.Duration
}

func (*fakeProcess) PID() int                { return 1 }
func (*fakeProcess) Stdin() io.WriteCloser   { return nopWriteCloser{Writer: io.Discard} }
func (p *fakeProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *fakeProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *fakeProcess) Wait() processruntime.Result {
	time.Sleep(p.delay)
	return p.result
}
func (*fakeProcess) Interrupt() error        { return nil }
func (*fakeProcess) KillTree() error         { return nil }
func (*fakeProcess) TreeKillSupported() bool { return true }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

type blockingControl struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingControl) CreateSession(_ context.Context, request CreateSessionRequest) (CreateSessionResult, bool, error) {
	return CreateSessionResult{SessionID: "native-session", Model: request.Model, ModelVerified: true}, true, nil
}
func (b *blockingControl) Prompt(context.Context, PromptRequest) (PromptResult, bool, error) {
	b.calls.Add(1)
	close(b.entered)
	<-b.release
	return PromptResult{Accepted: true, ReceiptID: "receipt"}, true, nil
}

func TestOpenStartAndSteerPreserveBindings(t *testing.T) {
	control := &fakeControl{}
	launcher := &fakeLauncher{output: `{"type":"text","sessionID":"native-session","part":{"id":"text-1","text":"hello"}}` + "\n"}
	adapter := New(Config{Control: control, Launcher: launcher, Now: func() time.Time { return time.Unix(1, 0) }, NewID: func(prefix string) (string, error) { return prefix + "-id", nil }})
	assignment := provider.AssignmentRef{ProjectID: "p", RunID: "r", AttemptID: "a", AssignmentID: "x", Revision: 2}
	model := provider.ModelSelection{ProviderID: "opencode", ModelID: "mimo-v2.5-free", Variant: "default"}
	contract := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{RequestID: "open", Assignment: assignment, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: model, ResultContract: contract, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	if control.create.Directory != "/work" || control.create.Model != model {
		t.Fatalf("create request lost binding: %#v", control.create)
	}
	started, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{RequestID: "start", Session: opened.Session.Ref(), Assignment: assignment, Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "do work"}}, ResultContract: contract})
	if err != nil {
		t.Fatal(err)
	}
	if launcher.spec.Dir != "/work" || !containsPair(launcher.spec.Args, "--model", "opencode/mimo-v2.5-free") || !containsPair(launcher.spec.Args, "--session", "native-session") {
		t.Fatalf("bad run spec: %#v", launcher.spec)
	}
	next := assignment
	next.Revision++
	receipt, err := opened.Session.Steer(context.Background(), provider.SteerRequest{RequestID: "steer", MessageID: "msg", Session: opened.Session.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: assignment, ExpectedRevision: 2, NewAssignment: next, Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Text: "amend"}}})
	if err != nil {
		t.Fatal(err)
	}
	if control.prompt.Directory != "/work" || control.prompt.Delivery != "steer" || receipt.Targeting != provider.TargetSessionLease {
		t.Fatalf("bad steer: %#v %#v", control.prompt, receipt)
	}
}

func TestSteerCancellationAfterDispatchIsUncertainAndDoesNotAdvanceRevision(t *testing.T) {
	control := &fakeControl{promptSent: true, promptErr: context.Canceled}
	launcher := &fakeLauncher{output: ""}
	adapter := New(Config{Control: control, Launcher: launcher})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	m := provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, _ := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: m, ResultContract: c, PermissionMode: provider.PermissionDeny})
	started, _ := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c})
	next := a
	next.Revision = 2
	_, err := opened.Session.Steer(context.Background(), provider.SteerRequest{Session: opened.Session.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: a, ExpectedRevision: 1, NewAssignment: next, Mode: provider.DeliverActiveTurn})
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCallerCancelled || providerErr.RemoteEffect != provider.RemoteEffectPossiblySent || providerErr.Retry != provider.RetryAfterReconcile {
		t.Fatalf("expected uncertain delivery, got %v", err)
	}
	_, _ = opened.Session.Steer(context.Background(), provider.SteerRequest{Session: opened.Session.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: a, ExpectedRevision: 1, NewAssignment: next, Mode: provider.DeliverActiveTurn})
	if control.promptCalls != 1 {
		t.Fatalf("uncertain delivery was retried: %d calls", control.promptCalls)
	}
}

func TestStartTurnPreCancelledDoesNotLaunch(t *testing.T) {
	control, launcher := &fakeControl{}, &fakeLauncher{}
	adapter := New(Config{Control: control, Launcher: launcher})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	m := provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: m, ResultContract: c, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = opened.Session.StartTurn(ctx, provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c})
	if err == nil || launcher.starts != 0 {
		t.Fatalf("cancelled start launched process: starts=%d err=%v", launcher.starts, err)
	}
}

func TestConcurrentSteerOnlyDispatchesOnce(t *testing.T) {
	control := &blockingControl{entered: make(chan struct{}), release: make(chan struct{})}
	launcher := &fakeLauncher{delay: time.Second}
	adapter := New(Config{Control: control, Launcher: launcher})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	next := a
	next.Revision = 2
	m := provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, _ := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: m, ResultContract: c, PermissionMode: provider.PermissionDeny})
	started, _ := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c})
	request := provider.SteerRequest{Session: opened.Session.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: a, ExpectedRevision: 1, NewAssignment: next, Mode: provider.DeliverActiveTurn}
	done := make(chan error, 1)
	go func() { _, err := opened.Session.Steer(context.Background(), request); done <- err }()
	<-control.entered
	if _, err := opened.Session.Steer(context.Background(), request); err == nil {
		t.Fatal("concurrent steer was accepted")
	}
	if control.calls.Load() != 1 {
		t.Fatalf("concurrent steer dispatched %d calls", control.calls.Load())
	}
	close(control.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNonzeroExitKeepsSessionLostAndBlocksNewTurn(t *testing.T) {
	control, launcher := &fakeControl{}, &fakeLauncher{result: processruntime.Result{ExitCode: 7, Err: errors.New("exit status 7")}}
	adapter := New(Config{Control: control, Launcher: launcher})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	m := provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, _ := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: m, ResultContract: c, PermissionMode: provider.PermissionDeny})
	_, _ = opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c})
	time.Sleep(100 * time.Millisecond)
	snapshot, _ := opened.Session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: opened.Session.Ref()})
	if snapshot.SessionState != provider.SessionLost {
		t.Fatalf("expected lost state, got %s", snapshot.SessionState)
	}
	_, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c})
	if err == nil || launcher.starts != 1 {
		t.Fatalf("lost session accepted replacement turn: starts=%d err=%v", launcher.starts, err)
	}
}

func TestOversizedJSONLMarksSessionLost(t *testing.T) {
	control, launcher := &fakeControl{}, &fakeLauncher{output: strings.Repeat("x", 128) + "\n"}
	adapter := New(Config{Control: control, Launcher: launcher, MaxTextBytes: 32})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	m := provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, _ := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: m, ResultContract: c, PermissionMode: provider.PermissionDeny})
	_, _ = opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c})
	time.Sleep(100 * time.Millisecond)
	snapshot, _ := opened.Session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: opened.Session.Ref()})
	if snapshot.SessionState != provider.SessionLost {
		t.Fatalf("oversized JSONL did not lose session: %s", snapshot.SessionState)
	}
}

func TestDetachRemainsClosedAfterRunEOF(t *testing.T) {
	control, launcher := &fakeControl{}, &fakeLauncher{delay: 100 * time.Millisecond}
	adapter := New(Config{Control: control, Launcher: launcher})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	m := provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, _ := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: m, ResultContract: c, PermissionMode: provider.PermissionDeny})
	_, _ = opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c})
	_, err := opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseExplicitDetach})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	snapshot, _ := opened.Session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: opened.Session.Ref()})
	if snapshot.SessionState != provider.SessionClosed {
		t.Fatalf("detached EOF reopened session: %s", snapshot.SessionState)
	}
}

func TestNormalizeWithholdsFinalPhaseAndReasoningText(t *testing.T) {
	s := &session{config: withDefaults(Config{NewID: func(prefix string) (string, error) { return prefix, nil }}), ref: provider.SessionRef{AdapterSessionID: "s"}, nativeSession: "n", assignment: provider.AssignmentRef{Revision: 1}, broker: eventstream.New(8)}
	stream, _ := s.broker.Subscribe()
	s.normalizeLine([]byte(`{"type":"reasoning","sessionID":"n","part":{"id":"r","text":"private"}}`), provider.TurnRef{})
	s.normalizeLine([]byte(`{"type":"text","sessionID":"n","part":{"id":"t","text":"answer"}}`), provider.TurnRef{})
	first, _ := stream.Receive(context.Background())
	second, _ := stream.Receive(context.Background())
	reasoning := first.Body().(provider.ReasoningSummaryEvent)
	text := second.Body().(provider.AssistantTextEvent)
	if reasoning.Update.Text != "" || reasoning.Availability != provider.ReasoningUnknown {
		t.Fatal("reasoning text leaked")
	}
	if text.Phase != provider.AssistantPhaseUnknown || text.Update.Mode != provider.UpdateSnapshot {
		t.Fatalf("unproven final/delta semantics: %#v", text)
	}
}

func TestCLIControlPromptUsesLowercasePayloadAndWorkspace(t *testing.T) {
	launcher := &fakeLauncher{output: `{"data":{"id":"message-1","sessionID":"native-session"}}`}
	control := &cliControl{executable: "opencode", launcher: launcher}
	result, sent, err := control.Prompt(context.Background(), PromptRequest{SessionID: "native-session", Directory: "/work", Text: "fix", Delivery: "steer"})
	if err != nil || !sent || !result.Accepted {
		t.Fatalf("prompt failed: %#v %v", result, err)
	}
	if launcher.spec.Dir != "/work" {
		t.Fatalf("prompt used wrong directory: %q", launcher.spec.Dir)
	}
	var payload string
	for i := range launcher.spec.Args {
		if launcher.spec.Args[i] == "--data" && i+1 < len(launcher.spec.Args) {
			payload = launcher.spec.Args[i+1]
		}
	}
	if payload != `{"text":"fix","delivery":"steer"}` {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func TestSessionIDFailurePreventsNativeCreate(t *testing.T) {
	control, launcher := &fakeControl{}, &fakeLauncher{}
	adapter := New(Config{Control: control, Launcher: launcher, NewID: func(string) (string, error) { return "", errors.New("entropy unavailable") }})
	_, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: provider.AssignmentRef{AssignmentID: "x"}, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}, ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone}, PermissionMode: provider.PermissionDeny})
	if err == nil || control.create.Directory != "" || launcher.starts != 0 {
		t.Fatalf("ID failure reached native side effect: create=%#v starts=%d err=%v", control.create, launcher.starts, err)
	}
}

func TestEventIDFailureTerminatesBroker(t *testing.T) {
	s := &session{config: withDefaults(Config{NewID: func(string) (string, error) { return "", errors.New("entropy unavailable") }}), ref: provider.SessionRef{AdapterSessionID: "s"}, broker: eventstream.New(1)}
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	s.emit(provider.SessionStateEvent{Previous: provider.SessionOpening, Current: provider.SessionOpen})
	_, err = stream.Receive(context.Background())
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Operation != provider.OperationReceiveEvent {
		t.Fatalf("expected terminal provider error, got %v", err)
	}
}

func containsPair(values []string, left, right string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == left && values[i+1] == right {
			return true
		}
	}
	return false
}
