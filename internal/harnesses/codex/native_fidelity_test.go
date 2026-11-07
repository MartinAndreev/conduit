package codex

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// drainEvents finishes the broker and returns every published event.
func drainEvents(t *testing.T, s *session, stream provider.EventStream) []provider.ProviderEvent {
	t.Helper()
	s.broker.Finish(nil)
	var events []provider.ProviderEvent
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		event, err := stream.Receive(ctx)
		cancel()
		if err == io.EOF {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func subscribe(t *testing.T, s *session) provider.EventStream {
	t.Helper()
	stream, err := s.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func send(t *testing.T, s *session, method string, params any) {
	t.Helper()
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	s.handleNotification(method, encoded)
}

func beginTurn(t *testing.T, s *session, turnID string) {
	t.Helper()
	committed := s.commitTurnStart(provider.StartTurnRequest{Assignment: s.assignment}, provider.SchemaNative, json.RawMessage(`{"turn":{"id":"`+turnID+`"}}`))
	if committed.err != nil {
		t.Fatal(committed.err)
	}
}

func completeTurn(t *testing.T, s *session, turnID string, turn map[string]any) {
	t.Helper()
	turn["id"] = turnID
	send(t, s, "turn/completed", map[string]any{"threadId": "thread-1", "turn": turn})
}

func breakdown(input, cached, output, reasoning uint64) map[string]any {
	return map[string]any{"inputTokens": input, "cachedInputTokens": cached, "cacheWriteInputTokens": 0, "outputTokens": output, "reasoningOutputTokens": reasoning, "totalTokens": input + output}
}

func usageParams(turnID string, total, last map[string]any) map[string]any {
	return map[string]any{"threadId": "thread-1", "turnId": turnID, "tokenUsage": map[string]any{"total": total, "last": last, "modelContextWindow": 258400}}
}

func usageEvents(events []provider.ProviderEvent) []provider.UsageEvent {
	var result []provider.UsageEvent
	for _, event := range events {
		if body, ok := event.Body().(provider.UsageEvent); ok {
			result = append(result, body)
		}
	}
	return result
}

func warnings(events []provider.ProviderEvent, code string) []provider.ProviderEvent {
	var result []provider.ProviderEvent
	for _, event := range events {
		if body, ok := event.Body().(provider.ProtocolWarningEvent); ok && body.Code == code {
			result = append(result, event)
		}
	}
	return result
}

func count(value provider.Optional[uint64]) any {
	if !value.Present {
		return "unknown"
	}
	return value.Value
}

// Numbers from the T3 multi_turn Codex fixture: tokenUsage.total is thread-wide,
// so turn 2's usage is its total minus the total observed when it began.
func TestTurnUsageSubtractsThreadTotalAtTurnStart(t *testing.T) {
	s := normalizationSession(t)
	stream := subscribe(t, s)
	beginTurn(t, s, "turn-1")
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-1", breakdown(21418, 6912, 8, 0), breakdown(21418, 6912, 8, 0)))
	completeTurn(t, s, "turn-1", map[string]any{"status": "completed", "error": nil})
	beginTurn(t, s, "turn-2")
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-2", breakdown(47195, 28160, 16, 0), breakdown(25777, 21248, 8, 0)))
	// A late total for the finished turn grows the thread total; it must not
	// be charged to turn 2.
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-1", breakdown(47205, 28160, 16, 0), breakdown(10, 0, 0, 0)))
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-2", breakdown(47305, 28160, 20, 0), breakdown(100, 0, 4, 0)))
	// A total below the baseline cannot yield a turn snapshot.
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-2", breakdown(50, 0, 1, 0), breakdown(50, 0, 1, 0)))
	events := drainEvents(t, s, stream)
	usage := usageEvents(events)
	if len(usage) != 4 {
		t.Fatalf("usage events = %d, want 4", len(usage))
	}
	want := []struct {
		input, cached, output any
		measurement           provider.UsageMeasurement
	}{
		{uint64(21418), uint64(6912), uint64(8), provider.UsageCumulativeSnapshot},
		{uint64(25777), uint64(21248), uint64(8), provider.UsageCumulativeSnapshot},
		{uint64(25877), uint64(21248), uint64(12), provider.UsageCumulativeSnapshot},
		{uint64(50), uint64(0), uint64(1), provider.UsageMeasurementUnknown},
	}
	for i, w := range want {
		got := usage[i]
		if count(got.Counters.InputTokens) != w.input || count(got.Counters.CacheReadTokens) != w.cached || count(got.Counters.OutputTokens) != w.output || got.Measurement != w.measurement {
			t.Fatalf("usage[%d] = in %v cached %v out %v %s, want %+v", i, count(got.Counters.InputTokens), count(got.Counters.CacheReadTokens), count(got.Counters.OutputTokens), got.Measurement, w)
		}
	}
	if len(warnings(events, "usage_total_regressed")) != 1 {
		t.Fatal("regressed total was not reported")
	}
}

// Counters absent from the native payload are unknown, not zero.
func TestAbsentUsageCountersAreUnknown(t *testing.T) {
	s := normalizationSession(t)
	stream := subscribe(t, s)
	beginTurn(t, s, "turn-1")
	total := map[string]any{"inputTokens": 10, "cachedInputTokens": 0, "outputTokens": 2, "reasoningOutputTokens": 1, "totalTokens": 12}
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-1", total, total))
	usage := usageEvents(drainEvents(t, s, stream))
	if len(usage) != 1 {
		t.Fatalf("usage events = %d", len(usage))
	}
	counters := usage[0].Counters
	if counters.CacheWriteTokens.Present {
		t.Fatalf("absent cacheWriteInputTokens reported as %d", counters.CacheWriteTokens.Value)
	}
	if !counters.CacheReadTokens.Present || counters.CacheReadTokens.Value != 0 || counters.ReasoningTokens.Value != 1 {
		t.Fatalf("reported counters = %#v", counters)
	}
}

func TestNativeErrorRetryAndFailureReasons(t *testing.T) {
	s := normalizationSession(t)
	stream := subscribe(t, s)
	beginTurn(t, s, "turn-1")
	retry := map[string]any{"threadId": "thread-1", "turnId": "turn-1", "willRetry": true, "error": map[string]any{"message": "stream disconnected", "codexErrorInfo": map[string]any{"responseStreamDisconnected": map[string]any{"httpStatusCode": nil}}}}
	send(t, s, "error", retry)
	send(t, s, "error", retry)
	send(t, s, "error", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "willRetry": false, "error": map[string]any{"message": "quota exhausted", "codexErrorInfo": "usageLimitExceeded"}})
	completeTurn(t, s, "turn-1", map[string]any{"status": "failed", "error": map[string]any{"message": "quota exhausted", "codexErrorInfo": "usageLimitExceeded"}})
	beginTurn(t, s, "turn-2")
	completeTurn(t, s, "turn-2", map[string]any{"status": "failed", "error": map[string]any{"message": "server overloaded"}})
	events := drainEvents(t, s, stream)
	if len(warnings(events, "unknown_notification")) != 0 {
		t.Fatal("error notification still treated as unknown")
	}
	retries := warnings(events, "native_error_retrying")
	if len(retries) != 1 || !strings.Contains(retries[0].Body().(provider.ProtocolWarningEvent).Detail.Text, "responseStreamDisconnected") {
		t.Fatalf("retry notices = %#v", retries)
	}
	failures := warnings(events, "native_turn_error")
	if len(failures) != 2 {
		t.Fatalf("turn error warnings = %d, want one per failed turn", len(failures))
	}
	first := failures[0].Body().(provider.ProtocolWarningEvent).Detail.Text
	second := failures[1].Body().(provider.ProtocolWarningEvent).Detail.Text
	if first != "usageLimitExceeded: quota exhausted" || second != "server overloaded" || failures[1].Envelope.Turn.Value.AdapterTurnID != "turn-2" {
		t.Fatalf("turn error details = %q, %q", first, second)
	}
	terminals := 0
	for _, event := range events {
		if body, ok := event.Body().(provider.TurnStateEvent); ok && body.Current == provider.TurnNativeTerminal {
			terminals++
			if body.TerminalReason != "failed" {
				t.Fatalf("terminal reason = %q", body.TerminalReason)
			}
		}
	}
	if terminals != 2 {
		t.Fatalf("terminal events = %d; a retried error must not end the turn", terminals)
	}
}

func TestFailedOrDeclinedItemsAreNotSuccessfulActivity(t *testing.T) {
	s := normalizationSession(t)
	stream := subscribe(t, s)
	beginTurn(t, s, "turn-1")
	changes := []map[string]any{{"path": "a.go", "kind": map[string]any{"type": "update"}}}
	for _, status := range []string{"failed", "declined"} {
		send(t, s, "item/completed", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "patch-" + status, "type": "fileChange", "status": status, "changes": changes}})
	}
	send(t, s, "item/completed", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "cmd-declined", "type": "commandExecution", "status": "declined", "command": "rm -rf x", "exitCode": nil}})
	send(t, s, "item/completed", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "cmd-failed", "type": "commandExecution", "status": "failed", "command": "false"}})
	send(t, s, "item/completed", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "cmd-exit", "type": "commandExecution", "status": "failed", "command": "false", "exitCode": 1}})
	events := drainEvents(t, s, stream)
	for _, event := range events {
		if event.Kind() == provider.EventFileActivity {
			t.Fatalf("failed or declined patch reported file activity: %#v", event.Body())
		}
	}
	if len(warnings(events, "file_change_not_applied")) != 2 || len(warnings(events, "command_declined")) != 1 || len(warnings(events, "command_failed")) != 1 {
		t.Fatalf("item status warnings missing: %d %d %d", len(warnings(events, "file_change_not_applied")), len(warnings(events, "command_declined")), len(warnings(events, "command_failed")))
	}
	completions := 0
	for _, event := range events {
		if body, ok := event.Body().(provider.CommandCompletedEvent); ok {
			completions++
			if body.CommandID == "cmd-exit" && (!body.ExitCode.Present || body.ExitCode.Value != 1) {
				t.Fatalf("exit code lost: %#v", body)
			}
			if body.CommandID == "cmd-declined" && body.ExitCode.Present {
				t.Fatalf("declined command reported exit code: %#v", body)
			}
		}
	}
	if completions != 3 {
		t.Fatalf("command completions = %d", completions)
	}
}

func TestThreadStartOmitsUnsupportedFallbackField(t *testing.T) {
	launcher := &scriptedLauncher{}
	adapter := New(Config{Launcher: launcher, CloseGrace: 20 * time.Millisecond})
	opened, err := adapter.OpenSession(context.Background(), provider.OpenSessionRequest{Assignment: testAssignment(1), Workspace: provider.WorkspaceBinding{Path: t.TempDir(), Writable: true}, Model: provider.ModelSelection{ModelID: "model"}, ResultContract: robustnessContract, PermissionMode: provider.PermissionDeny})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseInterruptThenClose})
	}()
	params, _ := launcher.request("thread/start")["params"].(map[string]any)
	if params == nil || params["model"] != "model" {
		t.Fatalf("thread/start params = %#v", params)
	}
	if _, present := params["allowProviderModelFallback"]; present {
		t.Fatal("thread/start sends allowProviderModelFallback, which Codex does not define")
	}
}

func TestModelRerouteAttributesLaterUsageAndWarns(t *testing.T) {
	s := normalizationSession(t)
	stream := subscribe(t, s)
	beginTurn(t, s, "turn-1")
	send(t, s, "model/rerouted", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "fromModel": "gpt-5.6-luna", "toModel": "gpt-6-astra", "reason": "highRiskCyberActivity"})
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-1", breakdown(10, 0, 1, 0), breakdown(10, 0, 1, 0)))
	completeTurn(t, s, "turn-1", map[string]any{"status": "completed"})
	beginTurn(t, s, "turn-2")
	send(t, s, "thread/tokenUsage/updated", usageParams("turn-2", breakdown(30, 0, 2, 0), breakdown(20, 0, 1, 0)))
	events := drainEvents(t, s, stream)
	reroutes := warnings(events, "model_rerouted")
	if len(reroutes) != 1 {
		t.Fatalf("reroute warnings = %d", len(reroutes))
	}
	detail := reroutes[0].Body().(provider.ProtocolWarningEvent).Detail.Text
	for _, part := range []string{"gpt-5.6-luna", "gpt-6-astra", "highRiskCyberActivity"} {
		if !strings.Contains(detail, part) {
			t.Fatalf("reroute detail %q lacks %q", detail, part)
		}
	}
	usage := usageEvents(events)
	if len(usage) != 2 || usage[0].Model.ModelID != "gpt-6-astra" || usage[0].Model.ProviderID != "openai" || usage[1].Model.ModelID != "gpt-5.6-luna" {
		t.Fatalf("usage models = %#v", usage)
	}
}

func TestThreadStatusSystemErrorAndLateIdle(t *testing.T) {
	status := func(kind string) map[string]any {
		return map[string]any{"threadId": "thread-1", "status": map[string]any{"type": kind}}
	}
	s := normalizationSession(t)
	stream := subscribe(t, s)
	send(t, s, "thread/status/changed", status("systemError"))
	if s.state != provider.SessionOpen {
		t.Fatalf("systemError changed state to %s", s.state)
	}
	for _, ended := range []provider.SessionState{provider.SessionClosing, provider.SessionLost, provider.SessionClosed} {
		s.state = ended
		s.closing = ended != provider.SessionLost
		send(t, s, "thread/status/changed", status("idle"))
		send(t, s, "thread/status/changed", status("active"))
		if s.state != ended {
			t.Fatalf("late native status overwrote %s with %s", ended, s.state)
		}
	}
	events := drainEvents(t, s, stream)
	if len(warnings(events, "native_thread_system_error")) != 1 {
		t.Fatal("systemError was not surfaced")
	}
	for _, event := range events {
		if event.Kind() == provider.EventSessionState {
			t.Fatalf("ignored status published %#v", event.Body())
		}
	}
}

func TestKnownAdministrativeNotificationsAreQuiet(t *testing.T) {
	s := normalizationSession(t)
	stream := subscribe(t, s)
	for _, method := range []string{"thread/started", "account/updated", "mcpServer/startupStatus/updated", "thread/settings/updated", "remoteControl/status/changed", "item/commandExecution/outputDelta"} {
		send(t, s, method, map[string]any{"threadId": "thread-1"})
	}
	warning := map[string]any{"summary": "Codex could not find bubblewrap on PATH.", "details": nil}
	send(t, s, "configWarning", warning)
	send(t, s, "configWarning", warning)
	send(t, s, "brand/new", map[string]any{})
	send(t, s, "brand/new", map[string]any{})
	events := drainEvents(t, s, stream)
	config := warnings(events, "native_config_warning")
	if len(config) != 1 || config[0].Body().(provider.ProtocolWarningEvent).Detail.Text != "Codex could not find bubblewrap on PATH." {
		t.Fatalf("config warnings = %#v", config)
	}
	if len(warnings(events, "unknown_notification")) != 1 || len(events) != 2 {
		t.Fatalf("events = %d, want one config warning and one unknown warning", len(events))
	}
}

// Codex's schema sends PatchChangeKind as an object; a completed patch in that
// shape is file activity.
func TestCompletedSchemaShapedFileChangeIsActivity(t *testing.T) {
	s := normalizationSession(t)
	stream := subscribe(t, s)
	beginTurn(t, s, "turn-1")
	changes := []map[string]any{{"path": "a.go", "kind": map[string]any{"type": "update", "move_path": nil}}, {"path": "b.go", "kind": map[string]any{"type": "add"}}}
	send(t, s, "item/completed", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "patch", "type": "fileChange", "status": "completed", "changes": changes}})
	var operations []provider.FileOperation
	for _, event := range drainEvents(t, s, stream) {
		if body, ok := event.Body().(provider.FileActivityEvent); ok {
			operations = append(operations, body.Operation)
		}
	}
	if len(operations) != 2 || operations[0] != provider.FileUpdate || operations[1] != provider.FileCreate {
		t.Fatalf("file operations = %v", operations)
	}
}
