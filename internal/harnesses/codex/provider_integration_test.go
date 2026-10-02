package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type scriptedLauncher struct {
	mu       sync.Mutex
	spec     processruntime.Spec
	requests []map[string]any
	starts   int
}

func (l *scriptedLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	l.mu.Lock()
	l.spec = spec
	l.starts++
	l.mu.Unlock()
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	proc := &scriptedProcess{
		stdin: clientInput, stdout: clientOutput, stderr: io.NopCloser(&emptyReader{}),
		done: make(chan struct{}), serverInput: serverInput, serverOutput: serverOutput,
	}
	go l.serve(proc)
	return proc, nil
}

func TestOpenSessionIDFailurePreventsLaunch(t *testing.T) {
	launcher := &scriptedLauncher{}
	adapter := New(Config{Launcher: launcher, NewID: func(string) (string, error) { return "", errors.New("entropy unavailable") }})
	_, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Workspace: provider.WorkspaceBinding{Path: t.TempDir(), Writable: true}, Model: provider.ModelSelection{ProviderID: "openai", ModelID: "model"}, ResultContract: provider.ResultContractRef{Schema: json.RawMessage(`{"type":"object"}`), Requested: provider.SchemaNative}, PermissionMode: provider.PermissionDeny})
	launcher.mu.Lock()
	starts := launcher.starts
	launcher.mu.Unlock()
	if err == nil || starts != 0 {
		t.Fatalf("ID failure launched process: starts=%d err=%v", starts, err)
	}
}

func (l *scriptedLauncher) serve(proc *scriptedProcess) {
	defer close(proc.done)
	defer proc.serverOutput.Close()
	scanner := bufio.NewScanner(proc.serverInput)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		l.mu.Lock()
		l.requests = append(l.requests, request)
		l.mu.Unlock()
		id, hasID := request["id"]
		if !hasID {
			continue
		}
		var result any = map[string]any{}
		switch request["method"] {
		case "thread/start":
			result = map[string]any{"thread": map[string]any{"id": "native-thread-1"}}
		case "turn/start":
			result = map[string]any{"turn": map[string]any{"id": "native-turn-1"}}
		case "turn/steer":
			result = map[string]any{"turnId": "native-turn-1"}
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		if request["method"] == "thread/start" {
			middle := len(response) / 2
			_, _ = proc.serverOutput.Write(response[:middle])
			_, _ = proc.serverOutput.Write(append(response[middle:], '\n'))
		} else {
			_, _ = proc.serverOutput.Write(append(response, '\n'))
		}
	}
}

func (l *scriptedLauncher) request(method string) map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, request := range l.requests {
		if request["method"] == method {
			return request
		}
	}
	return nil
}

type scriptedProcess struct {
	stdin        *io.PipeWriter
	stdout       *io.PipeReader
	stderr       io.ReadCloser
	serverInput  *io.PipeReader
	serverOutput *io.PipeWriter
	done         chan struct{}
	killOnce     sync.Once
}

func (*scriptedProcess) PID() int                { return 42 }
func (p *scriptedProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *scriptedProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *scriptedProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *scriptedProcess) Interrupt() error      { return nil }
func (*scriptedProcess) TreeKillSupported() bool { return true }
func (p *scriptedProcess) KillTree() error {
	select {
	case <-p.done:
		return os.ErrProcessDone
	default:
	}
	p.killOnce.Do(func() {
		_ = p.serverInput.Close()
		_ = p.serverOutput.Close()
	})
	return nil
}
func (p *scriptedProcess) Wait() processruntime.Result {
	<-p.done
	return processruntime.Result{ExitCode: 0, ExitedAt: time.Now()}
}

func TestAdapterOpenStartSteerAndCloseAgainstScriptedAppServer(t *testing.T) {
	launcher := &scriptedLauncher{}
	var ids atomic.Uint64
	adapter := New(Config{
		ExecutablePath: "/test/codex", Launcher: launcher, CloseGrace: 100 * time.Millisecond,
		NewID: func(prefix string) (string, error) { return prefix + "-" + jsonNumber(ids.Add(1)), nil },
		Now:   func() time.Time { return time.Unix(123, 0) },
	})
	assignment1 := testAssignment(1)
	contract := provider.ResultContractRef{
		Name: "result", Version: "1", Schema: json.RawMessage(`{"type":"object"}`),
		Requested: provider.SchemaNative,
	}
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{
		RequestID: "open-1", Assignment: assignment1,
		Workspace:      provider.WorkspaceBinding{WorkspaceID: "workspace-1", Path: t.TempDir(), Writable: true},
		Model:          provider.ModelSelection{ProviderID: "openai", ModelID: "gpt-5.6-luna"},
		ResultContract: contract, PermissionMode: provider.PermissionDeny,
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	session := opened.Session
	started, err := session.StartTurn(context.Background(), provider.StartTurnRequest{
		RequestID: "turn-1", Session: session.Ref(), Assignment: assignment1,
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "make a tiny edit"}},
		ResultContract: contract,
	})
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if started.Turn.AdapterTurnID != "native-turn-1" || started.ResultEnforcement != provider.SchemaNative {
		t.Fatalf("StartTurn result = %#v", started)
	}
	assignment2 := testAssignment(2)
	receipt, err := session.Steer(context.Background(), provider.SteerRequest{
		RequestID: "steer-1", MessageID: "message-1", Session: session.Ref(),
		ExpectedActiveTurn: started.Turn, CurrentAssignment: assignment1, ExpectedRevision: 1,
		NewAssignment: assignment2, Mode: provider.DeliverActiveTurn,
		Parts: []provider.InputPart{{Kind: provider.InputInstruction, Text: "amend the output"}},
	})
	if err != nil {
		t.Fatalf("Steer: %v", err)
	}
	if !receipt.DispatchedFor.Present || receipt.DispatchedFor.Value != assignment2 || receipt.Receipt != provider.ReceiptNativeKnown {
		t.Fatalf("Steer receipt = %#v", receipt)
	}

	turnStart := launcher.request("turn/start")
	params, _ := turnStart["params"].(map[string]any)
	if params["threadId"] != "native-thread-1" || params["model"] != "gpt-5.6-luna" || params["outputSchema"] == nil {
		t.Fatalf("turn/start params = %#v", params)
	}
	steer := launcher.request("turn/steer")
	steerParams, _ := steer["params"].(map[string]any)
	if steerParams["expectedTurnId"] != "native-turn-1" {
		t.Fatalf("turn/steer params = %#v", steerParams)
	}
	type closeOutcome struct {
		result provider.CloseSessionResult
		err    error
	}
	outcomes := make(chan closeOutcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			result, closeErr := session.Close(context.Background(), provider.CloseSessionRequest{
				RequestID: "close-1", Session: session.Ref(), Policy: provider.CloseInterruptThenClose,
			})
			outcomes <- closeOutcome{result: result, err: closeErr}
		}()
	}
	var closed provider.CloseSessionResult
	errorsSeen := 0
	for i := 0; i < 2; i++ {
		outcome := <-outcomes
		if outcome.err != nil {
			errorsSeen++
			continue
		}
		closed = outcome.result
	}
	if errorsSeen != 1 {
		t.Fatalf("concurrent Close errors = %d, want exactly one", errorsSeen)
	}
	if !closed.HandleClosed || !closed.ResourcesReleased || closed.ExecutionState != provider.ExecutionNone {
		t.Fatalf("Close result = %#v", closed)
	}
	if launcher.spec.Path != "/test/codex" || len(launcher.spec.Args) != 1 || launcher.spec.Args[0] != "app-server" {
		t.Fatalf("launch spec = %#v", launcher.spec)
	}
}

func TestSteerRejectsMismatchedNativeTurnReceiptAndKeepsBindingUnresolved(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	request := provider.SteerRequest{
		RequestID: "steer-1", ExpectedActiveTurn: s.active.Value,
		CurrentAssignment: testAssignment(1), NewAssignment: testAssignment(2), ExpectedRevision: 1,
	}
	s.steerPending = &pendingSteer{request: request}
	_, err := s.acceptSteer(request, json.RawMessage(`{"turnId":"turn-other"}`))
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorProtocolViolation {
		t.Fatalf("acceptSteer error = %v", err)
	}
	if s.steerPending == nil || s.assignment.Revision != 1 {
		t.Fatalf("mismatched receipt changed unresolved binding: pending=%v assignment=%#v", s.steerPending, s.assignment)
	}
}

func testAssignment(revision uint64) provider.AssignmentRef {
	return provider.AssignmentRef{ProjectID: "project-1", RunID: "run-1", AttemptID: "attempt-1", AssignmentID: "assignment-1", Revision: revision}
}

func normalizationSession(t *testing.T) *session {
	t.Helper()
	return &session{
		config: withDefaults(Config{NewID: func(prefix string) (string, error) { return prefix + "-test", nil }}),
		ref:    provider.SessionRef{AdapterSessionID: "session-1"},
		model:  provider.ModelSelection{ProviderID: "openai", ModelID: "gpt-5.6-luna"},
		contract: provider.ResultContractNegotiation{
			Contract: provider.ResultContractRef{Name: "result", Version: "1", Schema: json.RawMessage(`{"type":"object"}`), Requested: provider.SchemaNative},
			Actual:   provider.SchemaNative,
		},
		process: &rpcTestProcess{stdin: &recordingWriteCloser{}, stdout: io.NopCloser(&emptyReader{}), stderr: io.NopCloser(&emptyReader{})},
		broker:  eventstream.New(512), nativeThread: "thread-1", state: provider.SessionOpen,
		assignment: testAssignment(1), textVersions: make(map[string]uint64), textBytes: make(map[string]uint64),
		warnedNative: make(map[string]struct{}), exitDone: make(chan struct{}), closeDone: make(chan struct{}),
	}
}

func TestActiveTurnAndRequestBindingsRejectBeforeNativeSideEffects(t *testing.T) {
	s := normalizationSession(t)
	s.active = provider.Some(provider.TurnRef{AdapterTurnID: "turn-1"})
	contract := s.contract.Contract
	_, err := s.StartTurn(context.Background(), provider.StartTurnRequest{
		RequestID: "turn-2", Session: s.ref, Assignment: s.assignment,
		Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "second"}}, ResultContract: contract,
	})
	assertProviderCode(t, err, provider.ErrorStaleBinding)

	_, err = s.ObserveState(context.Background(), provider.ObserveStateRequest{Session: provider.SessionRef{AdapterSessionID: "foreign"}})
	assertProviderCode(t, err, provider.ErrorStaleBinding)
	_, err = s.InterruptTurn(context.Background(), provider.InterruptRequest{
		RequestID: "interrupt-1", Session: provider.SessionRef{AdapterSessionID: "foreign"},
		ExpectedActiveTurn: s.active.Value, Assignment: s.assignment,
	})
	assertProviderCode(t, err, provider.ErrorStaleBinding)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Close(ctx, provider.CloseSessionRequest{RequestID: "close-cancelled", Session: s.ref, Policy: provider.CloseInterruptThenClose})
	assertProviderCode(t, err, provider.ErrorCallerCancelled)
	if s.closing {
		t.Fatal("pre-cancelled Close mutated session state")
	}
}

func assertProviderCode(t *testing.T, err error, code provider.ErrorCode) {
	t.Helper()
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != code {
		t.Fatalf("error = %v, want provider code %q", err, code)
	}
}
