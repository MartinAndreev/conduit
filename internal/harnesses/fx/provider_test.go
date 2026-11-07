package fx

import (
	"bufio"
	"context"
	"encoding/json"
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

func TestDescriptorLaunchesFxACP(t *testing.T) {
	descriptor := Descriptor()
	if descriptor.ProviderName != "fx" || descriptor.Executable != "fx" || len(descriptor.Args) != 1 || descriptor.Args[0] != "acp" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if len(descriptor.Env) != 0 {
		t.Fatalf("fx must inherit its environment unchanged: %v", descriptor.Env)
	}
	if descriptor.ProviderConfigID != "provider" || descriptor.VariantConfigID != "effort" {
		t.Fatalf("fx selection mapping = %q/%q", descriptor.ProviderConfigID, descriptor.VariantConfigID)
	}
	if len(descriptor.SessionSettings) != 1 || descriptor.SessionSettings[0] != (acp.ConfigSetting{ConfigID: "mode", Value: "auto"}) {
		t.Fatalf("fx must require mode=auto: %+v", descriptor.SessionSettings)
	}
	if len(descriptor.RecognizedNotifications) != 0 {
		t.Fatalf("no fx extension notification is recorded: %v", descriptor.RecognizedNotifications)
	}
	command, args := VersionCommand()
	if command != "fx" || len(args) != 1 || args[0] != "--version" {
		t.Fatalf("version command = %s %v", command, args)
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"0.0.14":         "0.0.14",
		" 0.0.13\n":      "0.0.13",
		"fx 0.0.14":      "0.0.14",
		"v1.2.3-beta.1":  "1.2.3-beta.1",
		"fx v1.2.3+abc":  "1.2.3+abc",
		"":               "",
		"fx":             "",
		"0.0":            "",
		"0.0.14 extra":   "",
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
	if err != nil || descriptor.ProviderName != "fx" || descriptor.AdapterVersion != adapterVersion || descriptor.HarnessVersion != "unknown" {
		t.Fatalf("Inspect = %+v, %v", descriptor, err)
	}
	if steer := capability(descriptor.Capabilities, provider.CapabilitySteerActiveTurn); steer.Support != provider.SupportUnsupported {
		t.Fatalf("steer = %+v", steer)
	}
	if launcher.started() != 0 {
		t.Fatal("Inspect launched fx")
	}
}

func TestOpenSessionAndTurnAgainstFxFixture(t *testing.T) {
	launcher := &fakeLauncher{t: t, responses: loadResponses(t)}
	adapter := New(acp.Config{Launcher: launcher, StopGrace: 200 * time.Millisecond})
	workspace := t.TempDir()
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{
		RequestID: "open", Assignment: provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		Workspace:      provider.WorkspaceBinding{Path: workspace, Writable: true},
		Model:          provider.ModelSelection{ProviderID: "codex", ModelID: "vendor/model-a", Variant: "medium"},
		ResultContract: provider.ResultContractRef{Requested: provider.SchemaNone},
		PermissionMode: provider.PermissionDeny,
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	session := opened.Session
	spec := launcher.spec
	if spec.Path != "fx" || len(spec.Args) != 1 || spec.Args[0] != "acp" || spec.Dir != workspace || spec.Env != nil {
		t.Fatalf("launch spec = %+v", spec)
	}
	capabilities := session.Capabilities()
	if capabilities.HarnessVersion != "0.0.14" || capabilities.ProviderName != "fx" {
		t.Fatalf("capabilities = %+v", capabilities)
	}
	steer := capability(capabilities, provider.CapabilitySteerActiveTurn)
	if steer.Support != provider.SupportUnsupported || !strings.Contains(steer.Detail, "_meta.fx.steering") {
		t.Fatalf("steer = %+v", steer)
	}
	sets := launcher.paramsFor("session/set_config_option")
	if len(sets) != 4 || !strings.Contains(sets[0], `"configId":"provider","value":"codex"`) ||
		!strings.Contains(sets[1], `"configId":"model","value":"vendor/model-a"`) ||
		!strings.Contains(sets[2], `"configId":"effort","value":"medium"`) ||
		!strings.Contains(sets[3], `"configId":"mode","value":"auto"`) {
		t.Fatalf("set_config_option = %v", sets)
	}

	started, err := session.StartTurn(context.Background(), provider.StartTurnRequest{
		Session: session.Ref(), Assignment: provider.AssignmentRef{AssignmentID: "a-1", Revision: 1},
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "Respond with: fixture simple ok"}},
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
	var text string
	for {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		switch body := event.Body().(type) {
		case provider.AssistantTextEvent:
			text += body.Update.Text
		case provider.ProtocolWarningEvent:
			t.Fatalf("unexpected warning %+v", body)
		case provider.TurnStateEvent:
			if body.Current == provider.TurnNativeTerminal {
				if body.TerminalReason != "end_turn" || event.Envelope.Turn.Value != started.Turn || text != "fixture simple ok" {
					t.Fatalf("terminal %+v text %q", body, text)
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

// fakeLauncher runs an in-process fx agent that answers from the fixture.
type fakeLauncher struct {
	t         *testing.T
	responses map[string]json.RawMessage
	mu        sync.Mutex
	spec      processruntime.Spec
	starts    int
	methods   []string
	params    map[string][]string
	options   []map[string]any
}

func (l *fakeLauncher) paramsFor(method string) []string {
	var params []string
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		params = append(params, l.params[method]...)
		return nil
	})
	return params
}

// setOption updates the fake session's option and returns all options.
func (l *fakeLauncher) setOption(raw json.RawMessage) any {
	var request struct {
		ConfigID string `json:"configId"`
		Value    string `json:"value"`
	}
	_ = json.Unmarshal(raw, &request)
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
	}
	return map[string]any{"configOptions": l.options}
}

func (l *fakeLauncher) started() int {
	var starts int
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		starts = l.starts
		return nil
	})
	return starts
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

func (l *fakeLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	agentInput, clientInput := io.Pipe()
	clientOutput, agentOutput := io.Pipe()
	proc := &fakeProcess{stdin: clientInput, stdout: clientOutput, agentInput: agentInput, agentOutput: agentOutput, done: make(chan struct{})}
	_ = lockscope.WithMutexLock(&l.mu, func() error {
		l.spec = spec
		l.starts++
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
	scanner := bufio.NewScanner(proc.agentInput)
	for scanner.Scan() {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil || len(frame.ID) == 0 {
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
		if frame.Method == "session/prompt" {
			var updates []json.RawMessage
			_ = json.Unmarshal(l.responses["session/prompt:updates"], &updates)
			for _, update := range updates {
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fx-session-01", "update": update}})
			}
		}
		var result any = l.responses[frame.Method]
		if frame.Method == "session/set_config_option" {
			result = l.setOption(frame.Params)
		}
		write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
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

func (*fakeProcess) PID() int                { return 7 }
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
