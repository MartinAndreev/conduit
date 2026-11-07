package opencode

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// normalizedEvents feeds one native line and returns the bodies it emitted.
func normalizedEvents(t *testing.T, line string, want int) []provider.EventBody {
	t.Helper()
	s := &session{config: withDefaults(Config{NewID: func(prefix string) (string, error) { return prefix, nil }}), ref: provider.SessionRef{AdapterSessionID: "s"}, nativeSession: "n", workspace: "/work", broker: eventstream.New(16)}
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	s.normalizeLine([]byte(line), provider.TurnRef{})
	s.broker.Finish(nil)
	var bodies []provider.EventBody
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err != nil {
			break
		}
		bodies = append(bodies, event.Body())
	}
	if len(bodies) != want {
		t.Fatalf("emitted %d events, want %d: %#v", len(bodies), want, bodies)
	}
	return bodies
}

func toolCompleted(t *testing.T, bodies []provider.EventBody) provider.ToolCompletedEvent {
	t.Helper()
	for _, body := range bodies {
		if event, ok := body.(provider.ToolCompletedEvent); ok {
			return event
		}
	}
	t.Fatalf("no tool completion in %#v", bodies)
	return provider.ToolCompletedEvent{}
}

func commandCompleted(t *testing.T, bodies []provider.EventBody) provider.CommandCompletedEvent {
	t.Helper()
	for _, body := range bodies {
		if event, ok := body.(provider.CommandCompletedEvent); ok {
			return event
		}
	}
	t.Fatalf("no command completion in %#v", bodies)
	return provider.CommandCompletedEvent{}
}

// The bash part shape is copied from T3 Code's recorded
// opencode_running_child_approval transcript: the exit code is
// `state.metadata.exit`, not a nested `metadata.metadata.exit`.
func TestShellExitCodeReadsRecordedMetadataExit(t *testing.T) {
	bodies := normalizedEvents(t, `{"type":"tool_use","sessionID":"n","part":{"id":"tool-1","tool":"bash","state":{"status":"completed","input":{"command":"printf ok"},"output":"(no output)","metadata":{"output":"(no output)","exit":0,"truncated":false}}}}`, 5)
	exit := commandCompleted(t, bodies).ExitCode
	if !exit.Present || exit.Value != 0 {
		t.Fatalf("exit code not read from state.metadata.exit: %#v", exit)
	}
	if outcome := toolCompleted(t, bodies).Outcome; outcome != provider.ToolSucceeded {
		t.Fatalf("zero exit outcome = %s", outcome)
	}
}

func TestCompletedShellWithNonzeroExitIsFailed(t *testing.T) {
	bodies := normalizedEvents(t, `{"type":"tool_use","sessionID":"n","part":{"id":"tool-1","tool":"bash","state":{"status":"completed","input":{"command":"false"},"output":"","metadata":{"exit":1}}}}`, 4)
	if outcome := toolCompleted(t, bodies).Outcome; outcome != provider.ToolFailed {
		t.Fatalf("nonzero shell exit outcome = %s, want failed", outcome)
	}
	if exit := commandCompleted(t, bodies).ExitCode; !exit.Present || exit.Value != 1 {
		t.Fatalf("exit code = %#v", exit)
	}
}

func TestFileActivityReadsFilePathThenPathThenFile(t *testing.T) {
	for _, tc := range []struct {
		tool, input, want string
		op                provider.FileOperation
	}{
		{"edit", `{"filePath":"/work/a.go","path":"/work/b.go"}`, "/work/a.go", provider.FileUpdate},
		{"read", `{"filePath":"/work/r.go"}`, "/work/r.go", provider.FileRead},
		{"write", `{"path":"/work/p.go"}`, "/work/p.go", provider.FileUnknown},
		{"edit", `{"file":"/work/f.go"}`, "/work/f.go", provider.FileUpdate},
	} {
		bodies := normalizedEvents(t, `{"type":"tool_use","sessionID":"n","part":{"id":"tool-1","tool":"`+tc.tool+`","state":{"status":"completed","input":`+tc.input+`,"output":"ok"}}}`, 3)
		file, ok := bodies[2].(provider.FileActivityEvent)
		if !ok || file.NativePath != tc.want || file.Operation != tc.op {
			t.Fatalf("%s %s: file activity = %#v", tc.tool, tc.input, bodies[2])
		}
	}
}

func TestToolErrorTextBecomesFailedOutput(t *testing.T) {
	bodies := normalizedEvents(t, `{"type":"tool_use","sessionID":"n","part":{"id":"tool-1","tool":"grep","state":{"status":"error","input":{"pattern":"x"},"error":"pattern is invalid"}}}`, 2)
	completed := toolCompleted(t, bodies)
	if completed.Outcome != provider.ToolFailed || completed.Output.Text != "pattern is invalid" {
		t.Fatalf("tool error lost: %#v", completed)
	}
}

func TestToolErrorTextIsBounded(t *testing.T) {
	s := &session{config: withDefaults(Config{MaxTextBytes: 4, NewID: func(prefix string) (string, error) { return prefix, nil }}), ref: provider.SessionRef{AdapterSessionID: "s"}, nativeSession: "n", broker: eventstream.New(4)}
	stream, _ := s.broker.Subscribe()
	s.normalizeLine([]byte(`{"type":"tool_use","sessionID":"n","part":{"id":"tool-1","tool":"grep","state":{"status":"error","error":"pattern is invalid"}}}`), provider.TurnRef{})
	_, _ = stream.Receive(context.Background())
	event, _ := stream.Receive(context.Background())
	completed := event.Body().(provider.ToolCompletedEvent)
	if completed.Output.Text != "patt" || !completed.Output.Truncated || completed.Output.OriginalBytes.Value != uint64(len("pattern is invalid")) {
		t.Fatalf("error text not bounded: %#v", completed.Output)
	}
}

func TestAbortedToolIsCancelled(t *testing.T) {
	for _, state := range []string{
		`{"status":"error","input":{"command":"sleep 9"},"error":"Tool execution aborted"}`,
		`{"status":"error","input":{"command":"sleep 9"},"error":"stopped","metadata":{"interrupted":true}}`,
	} {
		bodies := normalizedEvents(t, `{"type":"tool_use","sessionID":"n","part":{"id":"tool-1","tool":"bash","state":`+state+`}}`, 5)
		if outcome := toolCompleted(t, bodies).Outcome; outcome != provider.ToolCancelled {
			t.Fatalf("aborted tool outcome = %s for %s", outcome, state)
		}
	}
}

func TestStartTurnSeparatesPromptFromFlags(t *testing.T) {
	control, launcher := &fakeControl{}, &fakeLauncher{}
	adapter := New(Config{Control: control, Launcher: launcher})
	a := provider.AssignmentRef{AssignmentID: "x", Revision: 1}
	m := provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}
	c := provider.ResultContractRef{Requested: provider.SchemaNone}
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: a, Workspace: provider.WorkspaceBinding{Path: "/work", Writable: true}, Model: m, ResultContract: c, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	_, err = opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{Session: opened.Session.Ref(), Assignment: a, ResultContract: c, Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "--help me"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--session", "native-session", "--format", "json", "--model", "opencode/model", "--", "--help me"}
	if !slices.Equal(launcher.spec.Args, want) {
		t.Fatalf("run args = %q, want %q", launcher.spec.Args, want)
	}
}

func TestCLIControlCreateSessionSendsWorkspacePermissionRules(t *testing.T) {
	launcher := &fakeLauncher{output: `{"data":{"id":"native-session","model":{"providerID":"opencode","id":"model"}}}`}
	control := &cliControl{executable: "opencode", launcher: launcher}
	_, _, err := control.CreateSession(context.Background(), CreateSessionRequest{Directory: "/work", Model: provider.ModelSelection{ProviderID: "opencode", ModelID: "model"}, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	for i := range launcher.spec.Args {
		if launcher.spec.Args[i] == "--data" && i+1 < len(launcher.spec.Args) {
			payload = launcher.spec.Args[i+1]
		}
	}
	want := `{"title":"t","model":{"providerID":"opencode","id":"model"},"location":{"directory":"/work"},"permissions":[` +
		`{"action":"*","resource":"*","effect":"allow"},` +
		`{"action":"external_directory","resource":"*","effect":"deny"},` +
		`{"action":"webfetch","resource":"*","effect":"deny"},` +
		`{"action":"websearch","resource":"*","effect":"deny"},` +
		`{"action":"question","resource":"*","effect":"deny"}]}`
	if payload != want {
		t.Fatalf("session.create payload\n got %s\nwant %s", payload, want)
	}
}
