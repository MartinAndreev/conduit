package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/acp"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestDescriptorLaunchesAntigravityACP(t *testing.T) {
	descriptor := Descriptor()
	if descriptor.ProviderName != "antigravity" || descriptor.AdapterVersion != adapterVersion ||
		descriptor.Executable != "agy" || len(descriptor.Args) != 1 || descriptor.Args[0] != "acp" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if len(descriptor.Env) != 0 {
		t.Fatalf("antigravity must inherit its environment unchanged: %v", descriptor.Env)
	}
	if descriptor.ProviderConfigID != "" || descriptor.VariantConfigID != "thought_level" {
		t.Fatalf("antigravity selection mapping = %q/%q", descriptor.ProviderConfigID, descriptor.VariantConfigID)
	}
	if len(descriptor.SessionSettings) != 1 || descriptor.SessionSettings[0] != (acp.ConfigSetting{ConfigID: "mode", Value: "auto"}) {
		t.Fatalf("antigravity must require mode=auto: %+v", descriptor.SessionSettings)
	}
	if len(descriptor.RecognizedNotifications) != 0 {
		t.Fatalf("no antigravity extension notification is recorded: %v", descriptor.RecognizedNotifications)
	}
	command, args := VersionCommand()
	if command != "agy" || len(args) != 1 || args[0] != "--version" {
		t.Fatalf("version command = %s %v", command, args)
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"1.5.13":         "1.5.13",
		" 1.5.13\n":      "1.5.13",
		"agy 1.5.13":     "1.5.13",
		"v1.5.13-beta.1": "1.5.13-beta.1",
		"1.5.13+build.1": "1.5.13+build.1",
		"agy v1.5.13":    "",
		"":               "",
		"agy":            "",
		"1.5":            "",
		"1.5.13 extra":   "",
		"latest release": "",
	}
	for input, want := range cases {
		if got := ParseVersion(input); got != want {
			t.Fatalf("ParseVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInspectKeepsSteeringUnsupportedWithoutLaunching(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	descriptor, err := New(acp.Config{Launcher: launcher}).Inspect(context.Background(), provider.InspectRequest{})
	if err != nil || descriptor.ProviderName != "antigravity" || descriptor.AdapterVersion != adapterVersion || descriptor.HarnessVersion != "unknown" {
		t.Fatalf("Inspect = %+v, %v", descriptor, err)
	}
	if steer := capability(descriptor.Capabilities, provider.CapabilitySteerActiveTurn); steer.Support != provider.SupportUnsupported {
		t.Fatalf("steer = %+v", steer)
	}
	if launcher.started() != 0 {
		t.Fatal("Inspect launched agy")
	}
}

func TestSnapshotReportsNativeContextDefaults(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	descriptor, err := New(acp.Config{Launcher: launcher}).Inspect(context.Background(), provider.InspectRequest{})
	if err != nil {
		t.Fatalf("Inspect = %v", err)
	}
	report := descriptor.Capabilities.NativeContext
	for _, source := range provider.NativeContextSources() {
		if got := report.SourceState(source); got != provider.NativeContextPossible {
			t.Fatalf("%s = %s, want possible", source, got)
		}
	}
	if report.ProtectedPaths != provider.ProtectedPathsUnknown {
		t.Fatalf("protected paths = %s, want unknown", report.ProtectedPaths)
	}
}

func TestOpenSessionAndTurnAgainstAntigravityFixture(t *testing.T) {
	launcher := &fakeLauncher{t: t, responses: loadResponses(t)}
	adapter := New(acp.Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
	workspace := t.TempDir()
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{
		RequestID:      "open",
		Assignment:     provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		Workspace:      provider.WorkspaceBinding{Path: workspace, Writable: true},
		Model:          provider.ModelSelection{ModelID: "gemini-3.8-flash", Variant: "low"},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
		PermissionMode: provider.PermissionDeny,
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	session := opened.Session
	spec := launcher.spec
	if spec.Path != "agy" || len(spec.Args) != 1 || spec.Args[0] != "acp" || spec.Dir != workspace || spec.Env != nil {
		t.Fatalf("launch spec = %+v", spec)
	}
	capabilities := session.Capabilities()
	if capabilities.HarnessVersion != "1.5.13" || capabilities.ProviderName != "antigravity" {
		t.Fatalf("capabilities = %+v", capabilities)
	}
	if steer := capability(capabilities, provider.CapabilitySteerActiveTurn); steer.Support != provider.SupportUnsupported {
		t.Fatalf("steer = %+v", steer)
	}
	sets := launcher.paramsFor("session/set_config_option")
	if len(sets) != 3 ||
		!strings.Contains(sets[0], `"configId":"model","value":"gemini-3.8-flash"`) ||
		!strings.Contains(sets[1], `"configId":"thought_level","value":"low"`) ||
		!strings.Contains(sets[2], `"configId":"mode","value":"auto"`) {
		t.Fatalf("set_config_option = %v", sets)
	}

	started, err := session.StartTurn(context.Background(), provider.StartTurnRequest{
		Session:        session.Ref(),
		Assignment:     provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "Create hello.txt and run echo hi"}},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
	})
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	stream, err := session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var text, thought string
	var sawFile, sawCmdStart, sawCmdDone, sawTurnUsage bool
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		switch body := event.Body().(type) {
		case provider.AssistantTextEvent:
			text += body.Update.Text
		case provider.ReasoningSummaryEvent:
			thought += body.Update.Text
		case provider.FileActivityEvent:
			if body.Operation == provider.FileUpdate && body.NativePath == "/workspace/hello.txt" {
				sawFile = true
			}
		case provider.CommandStartedEvent:
			if body.Display.Text == "echo hi" {
				sawCmdStart = true
			}
		case provider.CommandCompletedEvent:
			if body.Output.Text == "hi\n" {
				sawCmdDone = true
			}
		case provider.UsageEvent:
			if body.Scope == provider.UsagePrimaryTurn {
				sawTurnUsage = true
				if !body.Counters.InputTokens.Present || body.Counters.InputTokens.Value != 12202 ||
					!body.Counters.OutputTokens.Present || body.Counters.OutputTokens.Value != 113 ||
					!body.Counters.ReasoningTokens.Present || body.Counters.ReasoningTokens.Value != 376 ||
					!body.Counters.CacheReadTokens.Present || body.Counters.CacheReadTokens.Value != 7833 {
					t.Fatalf("usage = %+v", body.Counters)
				}
			}
		case provider.ProtocolWarningEvent:
			t.Fatalf("unexpected warning %+v", body)
		case provider.TurnStateEvent:
			if body.Current == provider.TurnNativeTerminal {
				if body.TerminalReason != "end_turn" || event.Envelope.Turn.Value != started.Turn ||
					text != "done" || thought != "checking fixture steps" ||
					!sawFile || !sawCmdStart || !sawCmdDone || !sawTurnUsage {
					t.Fatalf("terminal=%+v text=%q thought=%q file=%v cmd=%v/%v usage=%v",
						body, text, thought, sawFile, sawCmdStart, sawCmdDone, sawTurnUsage)
				}
				result, err := session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseRequireQuiescent})
				if err != nil || !result.ResourcesReleased {
					t.Fatalf("Close = %+v, %v", result, err)
				}
				if !launcher.sawMethod("session/close") {
					t.Fatal("advertised session/close was not sent")
				}
				return
			}
		}
	}
}

func TestInterruptTurnCancelsActivePrompt(t *testing.T) {
	launcher := &fakeLauncher{t: t, responses: loadResponses(t), waitCancelOnPrompt: true}
	adapter := New(acp.Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
	opened, err := adapter.OpenSession(context.Background(), openRequest(t.TempDir(), provider.ModelSelection{}))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	session := opened.Session
	defer func() {
		_, _ = session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseInterruptThenClose})
	}()

	started, err := session.StartTurn(context.Background(), provider.StartTurnRequest{
		Session:        session.Ref(),
		Assignment:     provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "Long task"}},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
	})
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if _, err := session.InterruptTurn(context.Background(), provider.InterruptRequest{
		RequestID:          "int-1",
		Session:            session.Ref(),
		Assignment:         provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		ExpectedActiveTurn: started.Turn,
	}); err != nil {
		t.Fatalf("InterruptTurn: %v", err)
	}

	stream, err := session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if body, ok := event.Body().(provider.TurnStateEvent); ok && body.Current == provider.TurnNativeTerminal {
			if body.TerminalReason != "cancelled" {
				t.Fatalf("terminal reason = %q, want cancelled", body.TerminalReason)
			}
			return
		}
	}
}

func TestPermissionRequestWithStringIDIsRejectedOnce(t *testing.T) {
	launcher := &fakeLauncher{t: t, responses: loadResponses(t), askPermissionOnPrompt: true}
	adapter := New(acp.Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
	opened, err := adapter.OpenSession(context.Background(), openRequest(t.TempDir(), provider.ModelSelection{}))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	session := opened.Session
	defer func() {
		_, _ = session.Close(context.Background(), provider.CloseSessionRequest{Session: session.Ref(), Policy: provider.CloseRequireQuiescent})
	}()

	if _, err := session.StartTurn(context.Background(), provider.StartTurnRequest{
		Session:        session.Ref(),
		Assignment:     provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "Trigger permission"}},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
	}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}

	stream, err := session.Events(context.Background(), provider.EventCursor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sawPermWarning := false
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		switch body := event.Body().(type) {
		case provider.ProtocolWarningEvent:
			if body.OffendingNativeKind == "session/request_permission" {
				sawPermWarning = true
			}
		case provider.TurnStateEvent:
			if body.Current == provider.TurnNativeTerminal {
				if !sawPermWarning {
					t.Fatal("expected protocol warning for rejected permission request")
				}
				reply := launcher.permissionReply()
				if !strings.Contains(reply, `"id":"1"`) || !strings.Contains(reply, `"optionId":"deny"`) {
					t.Fatalf("permission reply = %s", reply)
				}
				return
			}
		}
	}
}

func TestOpenSessionMapsUnauthenticatedError(t *testing.T) {
	launcher := &fakeLauncher{t: t, responses: loadResponses(t), unauthenticatedNew: true}
	adapter := New(acp.Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
	_, err := adapter.OpenSession(context.Background(), openRequest(t.TempDir(), provider.ModelSelection{}))
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) || pErr.Code != provider.ErrorAuthFailed {
		t.Fatalf("err = %v, want ErrorAuthFailed", err)
	}
	if !launcher.reaped() {
		t.Fatal("unauthenticated session process was not reaped")
	}
}

func TestOpenSessionFailsClosedOnUnsupportedModeProviderOrVariant(t *testing.T) {
	cases := []struct {
		name           string
		model          provider.ModelSelection
		omitAutoMode   bool
		rejectOptionID string
	}{
		{
			name:         "missing auto mode",
			omitAutoMode: true,
		},
		{
			name:  "non-empty provider is unsupported",
			model: provider.ModelSelection{ProviderID: "google"},
		},
		{
			name:  "unlisted thought_level variant",
			model: provider.ModelSelection{ModelID: "gemini-3.8-flash", Variant: "xhigh"},
		},
		{
			name:           "native -32602 rejection on thought_level",
			model:          provider.ModelSelection{ModelID: "gemini-3.8-flash-high", Variant: "xhigh"},
			rejectOptionID: "thought_level",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			launcher := &fakeLauncher{
				t:              t,
				responses:      loadResponses(t),
				omitAutoMode:   tc.omitAutoMode,
				rejectOptionID: tc.rejectOptionID,
			}
			adapter := New(acp.Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
			_, err := adapter.OpenSession(context.Background(), openRequest(t.TempDir(), tc.model))
			var pErr *provider.ProviderError
			if !errors.As(err, &pErr) || pErr.Code != provider.ErrorCapabilityUnsupported {
				t.Fatalf("err = %v, want ErrorCapabilityUnsupported", err)
			}
			if !launcher.reaped() {
				t.Fatal("aborted session process was not reaped")
			}
		})
	}
}

func openRequest(workspace string, model provider.ModelSelection) provider.OpenSessionRequest {
	return provider.OpenSessionRequest{
		RequestID:      "open",
		Assignment:     provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		Workspace:      provider.WorkspaceBinding{Path: workspace, Writable: true},
		Model:          model,
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
		PermissionMode: provider.PermissionDeny,
	}
}

func capability(snapshot provider.CapabilitySnapshot, name provider.Capability) provider.CapabilityObservation {
	for _, item := range snapshot.Items {
		if item.Capability == name {
			return item
		}
	}
	return provider.CapabilityObservation{}
}

func loadResponses(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/responses.json")
	if err != nil {
		t.Fatal(err)
	}
	var responses map[string]json.RawMessage
	if err := json.Unmarshal(raw, &responses); err != nil {
		t.Fatal(err)
	}
	return responses
}

// fakeLauncher runs an in-process Antigravity ACP agent driven by sanitized fixtures.
type fakeLauncher struct {
	t                     *testing.T
	responses             map[string]json.RawMessage
	unauthenticatedNew    bool
	omitAutoMode          bool
	rejectOptionID        string
	waitCancelOnPrompt    bool
	askPermissionOnPrompt bool

	mu        sync.Mutex
	spec      processruntime.Spec
	starts    int
	methods   []string
	params    map[string][]string
	options   []map[string]any
	permReply string
	lastProc  *fakeProcess
}

func (l *fakeLauncher) paramsFor(method string) []string {
	var params []string
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		params = append(params, l.params[method]...)
		return nil
	})
	return params
}

func (l *fakeLauncher) permissionReply() string {
	var reply string
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		reply = l.permReply
		return nil
	})
	return reply
}

func (l *fakeLauncher) started() int {
	var starts int
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		starts = l.starts
		return nil
	})
	return starts
}

func (l *fakeLauncher) reaped() bool {
	var proc *fakeProcess
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		proc = l.lastProc
		return nil
	})
	if proc == nil {
		return false
	}
	select {
	case <-proc.done:
		return true
	case <-time.After(time.Second):
		return false
	}
}

func (l *fakeLauncher) sawMethod(method string) bool {
	seen := false
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		for _, candidate := range l.methods {
			seen = seen || candidate == method
		}
		return nil
	})
	return seen
}

func (l *fakeLauncher) sessionNewResult() any {
	var created map[string]any
	_ = json.Unmarshal(l.responses["session/new"], &created)
	if !l.omitAutoMode {
		return created
	}
	rawOptions, _ := created["configOptions"].([]any)
	for _, item := range rawOptions {
		option, _ := item.(map[string]any)
		if option["id"] != "mode" {
			continue
		}
		option["options"] = []map[string]any{
			{"value": "default", "name": "Default"},
			{"value": "plan", "name": "Plan"},
		}
	}
	return created
}

// setOption mirrors agy 1.5.13: switching model refreshes thought_level options.
func (l *fakeLauncher) setOption(raw json.RawMessage) (any, *struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) {
	var request struct {
		ConfigID string `json:"configId"`
		Value    string `json:"value"`
	}
	_ = json.Unmarshal(raw, &request)
	if l.rejectOptionID != "" && request.ConfigID == l.rejectOptionID {
		return nil, &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{Code: -32602, Message: `Thinking level "` + request.Value + `" is not offered for model`}
	}
	if l.options == nil {
		var created struct {
			ConfigOptions []map[string]any `json:"configOptions"`
		}
		_ = json.Unmarshal(l.responses["session/new"], &created)
		l.options = created.ConfigOptions
	}
	for _, option := range l.options {
		if option["id"] == request.ConfigID {
			option["currentValue"] = request.Value
		}
		if request.ConfigID == "model" && request.Value == "gemini-3.8-flash" && option["id"] == "thought_level" {
			option["options"] = []map[string]any{
				{"value": "low", "name": "Low"},
				{"value": "medium", "name": "Medium"},
				{"value": "high", "name": "High"},
			}
		}
	}
	return map[string]any{"configOptions": l.options}, nil
}

func (l *fakeLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	agentInput, clientInput := io.Pipe()
	clientOutput, agentOutput := io.Pipe()
	proc := &fakeProcess{stdin: clientInput, stdout: clientOutput, agentInput: agentInput, agentOutput: agentOutput, done: make(chan struct{})}
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		l.spec = spec
		l.starts++
		l.lastProc = proc
		return nil
	})
	go l.serve(proc)
	return proc, nil
}

func (l *fakeLauncher) serve(proc *fakeProcess) {
	defer close(proc.done)
	defer proc.agentOutput.Close()
	write := func(value any) {
		encoded, _ := json.Marshal(value)
		_, _ = proc.agentOutput.Write(append(encoded, '\n'))
	}
	var pendingCancelPromptID json.RawMessage
	var pendingPermPromptID json.RawMessage
	scanner := bufio.NewScanner(proc.agentInput)
	for scanner.Scan() {
		rawLine := append([]byte(nil), scanner.Bytes()...)
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(rawLine, &frame) != nil {
			continue
		}
		if frame.Method == "" {
			if string(frame.ID) == `"1"` && len(pendingPermPromptID) != 0 {
				_ = lockscope.WithMutexLock(&l.mu, func() error {
					l.permReply = string(rawLine)
					return nil
				})
				write(map[string]any{"jsonrpc": "2.0", "id": pendingPermPromptID, "result": map[string]any{"stopReason": "end_turn"}})
				pendingPermPromptID = nil
			}
			continue
		}
		_ = lockscope.WithMutexLock(&l.mu, func() error {
			l.methods = append(l.methods, frame.Method)
			if l.params == nil {
				l.params = map[string][]string{}
			}
			l.params[frame.Method] = append(l.params[frame.Method], string(frame.Params))
			return nil
		})
		if frame.Method == "session/cancel" && len(pendingCancelPromptID) != 0 {
			write(map[string]any{"jsonrpc": "2.0", "id": pendingCancelPromptID, "result": map[string]any{"stopReason": "cancelled"}})
			pendingCancelPromptID = nil
			continue
		}
		if len(frame.ID) == 0 {
			continue
		}
		switch frame.Method {
		case "session/new":
			if l.unauthenticatedNew {
				write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": map[string]any{"code": -32000, "message": "Authentication required"}})
				continue
			}
			write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": l.sessionNewResult()})
		case "session/set_model":
			write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": map[string]any{}})
		case "session/set_config_option":
			result, rpcErr := l.setOption(frame.Params)
			if rpcErr != nil {
				write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": rpcErr})
				continue
			}
			write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
		case "session/prompt":
			if l.waitCancelOnPrompt {
				pendingCancelPromptID = frame.ID
				continue
			}
			if l.askPermissionOnPrompt {
				pendingPermPromptID = frame.ID
				write(map[string]any{
					"jsonrpc": "2.0",
					"id":      "1",
					"method":  "session/request_permission",
					"params": map[string]any{
						"sessionId": "agy-session-01",
						"options": []map[string]any{
							{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
							{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
						},
					},
				})
				continue
			}
			var updates []json.RawMessage
			_ = json.Unmarshal(l.responses["session/prompt:updates"], &updates)
			for _, update := range updates {
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "agy-session-01", "update": update}})
			}
			write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": l.responses["session/prompt"]})
		default:
			write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": l.responses[frame.Method]})
		}
	}
}

type fakeProcess struct {
	stdin       *io.PipeWriter
	stdout      *io.PipeReader
	agentInput  *io.PipeReader
	agentOutput *io.PipeWriter
	done        chan struct{}
	killOnce    sync.Once
}

func (*fakeProcess) PID() int                { return 9 }
func (p *fakeProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *fakeProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *fakeProcess) Stderr() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }
func (*fakeProcess) Interrupt() error        { return nil }
func (*fakeProcess) TreeKillSupported() bool { return true }
func (p *fakeProcess) KillTree() error {
	select {
	case <-p.done:
		return os.ErrProcessDone
	default:
	}
	p.killOnce.Do(func() {
		_ = p.agentInput.Close()
		_ = p.agentOutput.Close()
	})
	return nil
}
func (p *fakeProcess) Wait() processruntime.Result {
	<-p.done
	return processruntime.Result{ExitedAt: time.Now()}
}
