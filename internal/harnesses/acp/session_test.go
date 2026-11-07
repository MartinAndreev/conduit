package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/internal/eventstream"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// promptAgent answers each session/prompt with onPrompt.
func promptAgent(onPrompt func(*agentConn, clientFrame)) func(*agentConn) {
	return standardAgent(nil, nil, func(conn *agentConn, frame clientFrame) {
		if frame.Method == "session/prompt" {
			onPrompt(conn, frame)
		}
	})
}

func endTurn(conn *agentConn, frame clientFrame) {
	conn.reply(frame.ID, map[string]any{"stopReason": "end_turn"})
}

func TestLaunchUsesDescriptorExecutableArgsAndWorkspace(t *testing.T) {
	launcher := &fakeLauncher{serve: promptAgent(endTurn)}
	descriptor := testDescriptor()
	descriptor.Env = []string{"FAKE_ACP=1"}
	adapter := New(descriptor, Config{Launcher: launcher, StopGrace: 100 * time.Millisecond})
	workspace := t.TempDir()
	opened, err := adapter.OpenSession(context.Background(), openRequest(workspace))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref(), Policy: provider.CloseRequireQuiescent})
	spec := launcher.specs[0]
	if spec.Path != "fake-agent" || len(spec.Args) != 1 || spec.Args[0] != "acp" || spec.Dir != workspace || len(spec.Env) != 1 || spec.Env[0] != "FAKE_ACP=1" {
		t.Fatalf("spec = %+v", spec)
	}
	created := launcher.framesFor("session/new")
	if len(created) != 1 || !strings.Contains(string(created[0].Params), `"mcpServers":[]`) || !strings.Contains(string(created[0].Params), workspace) {
		t.Fatalf("session/new = %+v", created)
	}
}

func TestOpenRejectsUnsupportedPolicyBeforeLaunch(t *testing.T) {
	launcher := &fakeLauncher{serve: promptAgent(endTurn)}
	adapter := newTestAdapter(launcher, nil)
	request := openRequest(t.TempDir())
	request.PermissionMode = provider.PermissionAsk
	_, err := adapter.OpenSession(context.Background(), request)
	requireCode(t, err, provider.ErrorCapabilityUnsupported)
	request = openRequest(t.TempDir())
	request.ResultContract = provider.ResultContractRef{Requested: provider.SchemaNative, Schema: json.RawMessage(`{"type":"object"}`)}
	_, err = adapter.OpenSession(context.Background(), request)
	requireCode(t, err, provider.ErrorCapabilityUnsupported)
	if len(launcher.specs) != 0 {
		t.Fatal("rejected open launched the agent")
	}
}

func TestAuthRequiredMapsToAuthFailedAndReapsAgent(t *testing.T) {
	launcher := &fakeLauncher{serve: func(conn *agentConn) {
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch frame.Method {
			case "initialize":
				conn.reply(frame.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{map[string]any{"id": "login"}}})
			case "session/new":
				conn.replyError(frame.ID, -32000, "Authentication required", nil)
			}
		}
	}}
	adapter := newTestAdapter(launcher, nil)
	_, err := adapter.OpenSession(context.Background(), openRequest(t.TempDir()))
	requireCode(t, err, provider.ErrorAuthFailed)
	if !launcher.process(t).reaped() {
		t.Fatal("failed open left the agent running")
	}
}

func TestModelSelectionUnsupportedWhenNotAdvertised(t *testing.T) {
	launcher := &fakeLauncher{serve: promptAgent(endTurn)}
	adapter := newTestAdapter(launcher, nil)
	request := openRequest(t.TempDir())
	request.Model = provider.ModelSelection{ModelID: "vendor/model-z"}
	_, err := adapter.OpenSession(context.Background(), request)
	requireCode(t, err, provider.ErrorCapabilityUnsupported)
	if frames := append(launcher.framesFor("session/set_model"), launcher.framesFor("session/set_config_option")...); len(frames) != 0 {
		t.Fatalf("unadvertised selection was sent: %+v", frames)
	}
	if !launcher.process(t).reaped() {
		t.Fatal("failed open left the agent running")
	}
}

func TestModelSelectionUsesAdvertisedMethods(t *testing.T) {
	t.Run("set_model", func(t *testing.T) {
		created := map[string]any{"sessionId": testSessionID, "models": map[string]any{
			"currentModelId": "m-1", "availableModels": []any{map[string]any{"modelId": "m-1"}, map[string]any{"modelId": "m-2"}},
		}}
		serve := standardAgent(nil, created, func(conn *agentConn, frame clientFrame) {
			if frame.Method == "session/set_model" {
				conn.reply(frame.ID, map[string]any{})
			}
		})
		launcher := &fakeLauncher{serve: serve}
		request := openRequest(t.TempDir())
		request.Model = provider.ModelSelection{ModelID: "m-2"}
		opened, err := newTestAdapter(launcher, nil).OpenSession(context.Background(), request)
		if err != nil {
			t.Fatalf("OpenSession: %v", err)
		}
		defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref()})
		frames := launcher.framesFor("session/set_model")
		if len(frames) != 1 || !strings.Contains(string(frames[0].Params), `"modelId":"m-2"`) {
			t.Fatalf("set_model = %+v", frames)
		}
	})
	t.Run("config options", func(t *testing.T) {
		options := []any{
			map[string]any{"id": "provider", "category": "model", "type": "select", "options": []any{map[string]any{"value": "gateway"}, map[string]any{"value": "codex"}}},
			map[string]any{"id": "model", "category": "model", "type": "select", "options": []any{map[string]any{"group": "g", "options": []any{map[string]any{"value": "vendor/model-b"}}}}},
			map[string]any{"id": "mode", "category": "mode", "type": "select", "options": []any{map[string]any{"value": "codex"}}},
		}
		serve := standardAgent(nil, map[string]any{"sessionId": testSessionID, "configOptions": options}, func(conn *agentConn, frame clientFrame) {
			if frame.Method == "session/set_config_option" {
				conn.reply(frame.ID, map[string]any{"configOptions": options})
			}
		})
		launcher := &fakeLauncher{serve: serve}
		request := openRequest(t.TempDir())
		request.Model = provider.ModelSelection{ProviderID: "codex", ModelID: "vendor/model-b"}
		opened, err := newTestAdapter(launcher, nil).OpenSession(context.Background(), request)
		if err != nil {
			t.Fatalf("OpenSession: %v", err)
		}
		defer opened.Session.Close(context.Background(), provider.CloseSessionRequest{Session: opened.Session.Ref()})
		frames := launcher.framesFor("session/set_config_option")
		if len(frames) != 2 || !strings.Contains(string(frames[0].Params), `"configId":"provider","value":"codex"`) ||
			!strings.Contains(string(frames[1].Params), `"configId":"model","value":"vendor/model-b"`) {
			t.Fatalf("set_config_option = %+v", frames)
		}
	})
}

func TestPermissionWithoutRejectOnceIsCancelled(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		conn.request(7, "session/request_permission", map[string]any{
			"sessionId": testSessionID,
			"toolCall":  map[string]any{"toolCallId": "call-1", "title": "Run"},
			"options":   []any{map[string]any{"optionId": "yes", "kind": "allow_once"}, map[string]any{"optionId": "always", "kind": "allow_always"}},
		})
		answer, _ := conn.next()
		if !strings.Contains(string(answer.Result), `"outcome":"cancelled"`) || strings.Contains(string(answer.Result), "optionId") {
			conn.replyError(frame.ID, -32603, "unexpected answer "+string(answer.Result), nil)
			return
		}
		endTurn(conn, frame)
	}), nil)
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings(events, "permission_auto_cancelled")) != 1 || len(warnings(events, "native_turn_error")) != 0 {
		t.Fatalf("events:\n%s", describe(events))
	}
}

func TestUnknownAgentRequestIsRejectedAndWarnedOnce(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		for id := 1; id <= 2; id++ {
			conn.request(id, "fs/read_text_file", map[string]any{"sessionId": testSessionID, "path": "x"})
			answer, _ := conn.next()
			if !strings.Contains(string(answer.Error), "-32601") {
				conn.replyError(frame.ID, -32603, "unexpected answer", nil)
				return
			}
		}
		endTurn(conn, frame)
	}), nil)
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if got := warnings(events, "unsupported_agent_request"); len(got) != 1 || got[0].OffendingNativeKind != "fs/read_text_file" {
		t.Fatalf("events:\n%s", describe(events))
	}
	if len(warnings(events, "native_turn_error")) != 0 {
		t.Fatalf("agent did not see a rejection:\n%s", describe(events))
	}
}

func TestUnknownUpdatesAndNotificationsWarnOncePerKind(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		for range 2 {
			conn.update(testSessionID, map[string]any{"sessionUpdate": "plan", "entries": []any{}})
			conn.update(testSessionID, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "ask"})
			conn.update(testSessionID, map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": "echo"}})
			conn.update(testSessionID, map[string]any{"sessionUpdate": "config_option_update", "configOptions": []any{}})
			conn.notify("_vendor/progress", map[string]any{})
			conn.update("other-session", map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "foreign"}})
		}
		endTurn(conn, frame)
	}), nil)
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	unknown := warnings(events, "unknown_session_update")
	if len(unknown) != 2 || unknown[0].OffendingNativeKind != "plan" || unknown[1].OffendingNativeKind != "current_mode_update" {
		t.Fatalf("unknown updates = %+v", unknown)
	}
	if len(warnings(events, "unknown_notification")) != 1 || len(warnings(events, "uncorrelated_session_update")) != 1 {
		t.Fatalf("events:\n%s", describe(events))
	}
	if texts := bodiesOf[provider.AssistantTextEvent](events); len(texts) != 0 {
		t.Fatalf("echo or foreign text emitted: %+v", texts)
	}
}

func TestExecuteToolMapsToCommandEvents(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		conn.update(testSessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "cmd-1", "title": "run_terminal_command", "rawInput": map[string]any{"command": "echo ok", "cwd": "sub"}})
		conn.update(testSessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "cmd-1", "kind": "execute", "status": "in_progress", "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "ok\n"}}}})
		conn.update(testSessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "cmd-2", "title": "Execute", "kind": "execute", "status": "pending", "rawInput": map[string]any{"command": "false"}})
		conn.update(testSessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "cmd-2", "status": "in_progress", "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "partial"}}}})
		conn.update(testSessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "cmd-2", "status": "failed"})
		conn.update(testSessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "cmd-2", "status": "failed"})
		endTurn(conn, frame)
	}), nil)
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	// cmd-1 had no kind at first sight, so it stays a generic tool.
	if starts := bodiesOf[provider.ToolStartedEvent](events); len(starts) != 1 || starts[0].ToolCallID != "cmd-1" {
		t.Fatalf("tool starts = %+v", starts)
	}
	commands := bodiesOf[provider.CommandStartedEvent](events)
	if len(commands) != 1 || commands[0].CommandID != "cmd-2" || commands[0].Display.Text != "false" {
		t.Fatalf("command starts = %+v", commands)
	}
	if output := bodiesOf[provider.CommandOutputEvent](events); len(output) != 1 || output[0].Output.Text != "partial" {
		t.Fatalf("command output = %+v", output)
	}
	completed := bodiesOf[provider.CommandCompletedEvent](events)
	if len(completed) != 1 || completed[0].ExitCode.Present || len(warnings(events, "command_failed")) != 1 {
		t.Fatalf("command completion:\n%s", describe(events))
	}
}

func TestLineBoundLosesSessionWithoutTerminal(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		conn.update(testSessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": strings.Repeat("x", 4096)}})
	}), func(config *Config) { config.MaxLineBytes = 1024 })
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	assertSessionLost(t, h)
}

func TestMalformedLineLosesSession(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		conn.writeRaw("not json\n")
	}), nil)
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	assertSessionLost(t, h)
}

func TestProcessEOFLosesSession(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		_ = conn.output.Close()
	}), nil)
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	assertSessionLost(t, h)
}

func assertSessionLost(t *testing.T, h *testHarness) {
	t.Helper()
	events, err := h.collectUntil(func(provider.ProviderEvent) bool { return false })
	requireCode(t, err, provider.ErrorSessionLost)
	for _, turn := range bodiesOf[provider.TurnStateEvent](events) {
		if turn.Current == provider.TurnNativeTerminal {
			t.Fatalf("invented native terminal:\n%s", describe(events))
		}
	}
	turns := bodiesOf[provider.TurnStateEvent](events)
	if len(turns) == 0 || turns[len(turns)-1].Current != provider.TurnLost {
		t.Fatalf("turn not lost:\n%s", describe(events))
	}
	states := bodiesOf[provider.SessionStateEvent](events)
	if states[len(states)-1].Current != provider.SessionLost {
		t.Fatalf("session states = %+v", states)
	}
	proc := h.launcher.process(t)
	eventually(t, func() bool { return proc.kills.Load() > 0 && proc.reaped() }, "lost agent termination")
	_, err = h.startTurn(context.Background(), "again")
	requireCode(t, err, provider.ErrorSessionLost)
}

func TestConcurrentTurnIsRejected(t *testing.T) {
	release := make(chan struct{})
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		<-release
		endTurn(conn, frame)
	}), nil)
	if _, err := h.startTurn(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	_, err := h.startTurn(context.Background(), "second")
	requireCode(t, err, provider.ErrorStaleBinding)
	close(release)
	if _, err := h.collectUntil(isTurnTerminal); err != nil {
		t.Fatal(err)
	}
	if _, err := h.startTurn(context.Background(), "third"); err != nil {
		t.Fatalf("turn after terminal: %v", err)
	}
}

func TestCallerCancellationWhilePromptInFlight(t *testing.T) {
	release := make(chan struct{})
	serve := func(conn *agentConn) {
		for {
			frame, ok := conn.next()
			if !ok {
				return
			}
			switch frame.Method {
			case "initialize":
				conn.reply(frame.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
			case "session/new":
				conn.reply(frame.ID, map[string]any{"sessionId": testSessionID})
				// Stop reading so the session/prompt write blocks in flight.
				<-release
			case "session/prompt":
				endTurn(conn, frame)
			}
		}
	}
	h := openHarness(t, serve, nil)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.startTurn(cancelled, "never sent")
	if providerErr := requireCode(t, err, provider.ErrorCallerCancelled); providerErr.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("pre-dispatch cancel = %+v", providerErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err = h.startTurn(ctx, "in flight")
	providerErr := requireCode(t, err, provider.ErrorCallerCancelled)
	if providerErr.RemoteEffect != provider.RemoteEffectPossiblySent || providerErr.Retry != provider.RetryAfterReconcile {
		t.Fatalf("in-flight cancel = %+v", providerErr)
	}
	snapshot, _ := h.session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: h.session.Ref()})
	if !snapshot.ActiveTurn.Present {
		t.Fatal("possibly sent prompt released its turn")
	}
	_, err = h.startTurn(context.Background(), "blocked")
	requireCode(t, err, provider.ErrorStaleBinding)

	close(release)
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	turns := bodiesOf[provider.TurnStateEvent](events)
	if len(turns) != 1 || turns[0].Previous != provider.TurnStarting || turns[0].TerminalReason != "end_turn" {
		t.Fatalf("late turn states = %+v", turns)
	}
	eventually(t, func() bool {
		snapshot, _ := h.session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: h.session.Ref()})
		return !snapshot.ActiveTurn.Present
	}, "late reply to resolve the turn")
}

func TestLateSubscriberSeesBufferedEvents(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		conn.update(testSessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "hi"}})
		endTurn(conn, frame)
	}), nil)
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		snapshot, _ := h.session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: h.session.Ref()})
		return !snapshot.ActiveTurn.Present
	}, "turn to finish before subscribing")
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatal(err)
	}
	states := bodiesOf[provider.SessionStateEvent](events)
	if len(states) < 2 || states[0].Current != provider.SessionOpening || states[1].Current != provider.SessionOpen {
		t.Fatalf("startup events lost:\n%s", describe(events))
	}
	if len(bodiesOf[provider.AssistantTextEvent](events)) != 1 {
		t.Fatalf("text lost:\n%s", describe(events))
	}
	for index, event := range events {
		if event.Envelope.AdapterSeq != uint64(index+1) {
			t.Fatalf("sequence gap at %d: %d", index, event.Envelope.AdapterSeq)
		}
	}
}

func TestBackpressureLosesSessionAndStopsAgent(t *testing.T) {
	h := openHarness(t, promptAgent(func(conn *agentConn, frame clientFrame) {
		for range 20 {
			conn.update(testSessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "x"}})
		}
		endTurn(conn, frame)
	}), func(config *Config) { config.EventBuffer = 4 })
	if _, err := h.startTurn(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	proc := h.launcher.process(t)
	eventually(t, func() bool { return proc.kills.Load() > 0 }, "agent termination after overflow")
	_, err := h.collectUntil(func(provider.ProviderEvent) bool { return false })
	requireCode(t, err, provider.ErrorBackpressureExceeded)
	if !errors.Is(err, eventstream.ErrBufferOverflow) {
		t.Fatalf("overflow cause lost: %v", err)
	}
}

func TestUnsupportedOperationsAreTyped(t *testing.T) {
	h := openHarness(t, promptAgent(endTurn), nil)
	_, err := h.session.Steer(context.Background(), provider.SteerRequest{Session: h.session.Ref(), Mode: provider.DeliverActiveTurn})
	requireCode(t, err, provider.ErrorCapabilityUnsupported)
	_, err = h.session.RespondPermission(context.Background(), provider.PermissionResponse{})
	requireCode(t, err, provider.ErrorCapabilityUnsupported)
	_, err = h.session.AnswerQuestion(context.Background(), provider.QuestionAnswer{})
	requireCode(t, err, provider.ErrorCapabilityUnsupported)
	_, err = h.session.Events(context.Background(), provider.EventCursor{AdapterCursor: "1"})
	requireCode(t, err, provider.ErrorCapabilityUnsupported)
	if _, ok := any(h.adapter).(provider.ResumableProvider); ok {
		t.Fatal("ACP adapter must not offer resume")
	}
	_, err = h.session.InterruptTurn(context.Background(), provider.InterruptRequest{Session: h.session.Ref(), Assignment: testAssignment})
	requireCode(t, err, provider.ErrorStaleBinding)
	for _, item := range h.session.Capabilities().Items {
		if item.Capability == provider.CapabilitySessionResume && item.Support != provider.SupportUnsupported {
			t.Fatalf("resume capability = %+v", item)
		}
	}
}

func TestInspectDoesNotLaunch(t *testing.T) {
	launcher := &fakeLauncher{}
	descriptor, err := newTestAdapter(launcher, nil).Inspect(context.Background(), provider.InspectRequest{})
	if err != nil || descriptor.ProviderName != "fake" || descriptor.HarnessVersion != "unknown" || descriptor.ProtocolVersion != "acp/1" {
		t.Fatalf("Inspect = %+v, %v", descriptor, err)
	}
	if len(launcher.specs) != 0 {
		t.Fatal("Inspect launched the agent")
	}
}
