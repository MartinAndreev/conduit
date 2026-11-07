package claude

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func echoAndStart(f *fakeClaude, message map[string]any) {
	uuid := messageUUID(message)
	f.write(`{"type":"command_lifecycle","command_uuid":"` + uuid + `","state":"started","session_id":"` + f.nativeSession() + `"}`)
	f.write(`{"type":"user","message":{"role":"user","content":"do work"},"session_id":"` + f.nativeSession() + `","parent_tool_use_id":null,"uuid":"` + uuid + `","isReplay":true}`)
}

func resultLine(f *fakeClaude, reason string, uuids ...string) string {
	return `{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"` + f.nativeSession() + `","terminal_reason":"` + reason + `","user_message_uuids":["` + strings.Join(uuids, `","`) + `"]}`
}

func observe(t *testing.T, s provider.Session) provider.SessionSnapshot {
	t.Helper()
	snapshot, err := s.ObserveState(context.Background(), provider.ObserveStateRequest{Session: s.Ref()})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// The live no-login turn: echo and lifecycle prove delivery; a synthetic
// assistant message is a notice; is_error ends the turn as a native failure.
func TestTurnWithEchoAndErrorResult(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		f.writeFixture("turn_api_error.ndjson", map[string]string{"INPUT": messageUUID(message)})
	}
	s, stream := openTestSession(t, fake, noneContract)
	started := startTurn(t, s, noneContract)
	events := receiveUntil(t, stream, isTurnTerminal)

	sent := fake.messages("user")[0]
	if sent["session_id"] != fake.nativeSession() || sent["parent_tool_use_id"] != nil || sent["priority"] != nil || messageUUID(sent) == "" {
		t.Fatalf("sent user message = %#v", sent)
	}
	if !started.Native.Turn.Present || string(started.Native.Turn.Value) != messageUUID(sent) || started.TargetingStrength != provider.TargetSessionLease {
		t.Fatalf("start result = %#v", started)
	}
	turnStates := bodiesOf[provider.TurnStateEvent](events)
	if len(turnStates) != 2 || turnStates[0].Current != provider.TurnActive || turnStates[1].TerminalReason != "api_error" {
		t.Fatalf("turn states = %#v", turnStates)
	}
	if !hasWarning(events, "native_notice") || !hasWarning(events, "native_turn_error") {
		t.Fatalf("warnings = %q", warningCodes(events))
	}
	if len(bodiesOf[provider.AssistantTextEvent](events)) != 0 {
		t.Fatal("synthetic assistant message became model output")
	}
	for _, warning := range bodiesOf[provider.ProtocolWarningEvent](events) {
		if warning.Code == "native_turn_error" && !strings.Contains(warning.Detail.Text, "api_error") {
			t.Fatalf("turn error detail = %q", warning.Detail.Text)
		}
	}
	for _, event := range events {
		if _, startup := event.Body().(provider.SessionStateEvent); startup {
			continue
		}
		if event.Envelope.Turn.Value != started.Turn || !event.Envelope.Assignment.Present {
			t.Fatalf("event %s binding = %#v", event.Kind(), event.Envelope)
		}
	}
	usage := bodiesOf[provider.UsageEvent](events)
	if len(usage) != 1 || usage[0].Measurement != provider.UsageCumulativeSnapshot || !usage[0].Counters.InputTokens.Present || usage[0].Counters.Cost.Present {
		t.Fatalf("usage = %#v", usage)
	}
	if snapshot := observe(t, s); snapshot.ActiveTurn.Present {
		t.Fatalf("turn still active after terminal result: %#v", snapshot)
	}
	// The next turn is allowed after the terminal result.
	startTurn(t, s, noneContract)
}

func TestTurnNormalizesTextToolsAndStructuredOutput(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		f.writeFixture("turn_tools.ndjson", map[string]string{"INPUT": messageUUID(message)})
	}
	s, stream := openTestSession(t, fake, schemaContract)
	startTurn(t, s, schemaContract)
	events := receiveUntil(t, stream, isTurnTerminal)

	reasoning := bodiesOf[provider.ReasoningSummaryEvent](events)
	if len(reasoning) != 3 {
		t.Fatalf("reasoning events = %#v", reasoning)
	}
	assertUpdate(t, reasoning[0].Update, "msg_a#0", "thinking", provider.UpdateDelta, 1, 0, "Plan")
	assertUpdate(t, reasoning[1].Update, "msg_a#0", "thinking", provider.UpdateDelta, 2, 4, " it")
	assertUpdate(t, reasoning[2].Update, "msg_a#0", "thinking", provider.UpdateSnapshot, 3, 0, "Plan it")
	if reasoning[1].Update.BaseVersion.Value != 1 || reasoning[2].Update.BaseVersion.Present {
		t.Fatalf("reasoning base versions = %#v", reasoning)
	}
	text := bodiesOf[provider.AssistantTextEvent](events)
	if len(text) != 4 {
		t.Fatalf("text events = %#v", text)
	}
	assertUpdate(t, text[0].Update, "msg_a#1", "text", provider.UpdateDelta, 1, 0, "Hel")
	assertUpdate(t, text[1].Update, "msg_a#1", "text", provider.UpdateDelta, 2, 3, "lo")
	assertUpdate(t, text[2].Update, "msg_a#1", "text", provider.UpdateSnapshot, 3, 0, "Hello")
	assertUpdate(t, text[3].Update, "msg_f#0", "text", provider.UpdateSnapshot, 1, 0, "Done")

	commandsStarted := bodiesOf[provider.CommandStartedEvent](events)
	commandsCompleted := bodiesOf[provider.CommandCompletedEvent](events)
	if len(commandsStarted) != 2 || commandsStarted[0].CommandID != "toolu_bash_ok" || commandsStarted[0].Display.Text != "go test ./..." || commandsStarted[0].CWD == "" {
		t.Fatalf("command starts = %#v", commandsStarted)
	}
	if len(commandsCompleted) != 2 || commandsCompleted[0].Output.Text != "ok all tests" || commandsCompleted[0].ExitCode.Present || commandsCompleted[1].CommandID != "toolu_bash_fail" {
		t.Fatalf("command completions = %#v", commandsCompleted)
	}
	failedIndex, completedIndex := -1, -1
	for index, event := range events {
		if warning, ok := event.Body().(provider.ProtocolWarningEvent); ok && warning.Code == "command_failed" {
			failedIndex = index
		}
		if completed, ok := event.Body().(provider.CommandCompletedEvent); ok && completed.CommandID == "toolu_bash_fail" {
			completedIndex = index
		}
	}
	if failedIndex < 0 || failedIndex > completedIndex {
		t.Fatalf("command_failed warning at %d, failed completion at %d", failedIndex, completedIndex)
	}

	files := bodiesOf[provider.FileActivityEvent](events)
	if len(files) != 1 || files[0].NativePath != "/workspace/main.go" || files[0].Operation != provider.FileUpdate || files[0].ToolCallID.Value != "toolu_edit" {
		t.Fatalf("file activity = %#v", files)
	}
	if !hasWarning(events, "file_change_not_applied") {
		t.Fatalf("failed Write did not report file_change_not_applied: %q", warningCodes(events))
	}
	toolsStarted := bodiesOf[provider.ToolStartedEvent](events)
	toolsCompleted := bodiesOf[provider.ToolCompletedEvent](events)
	if len(toolsStarted) != 3 || toolsStarted[2].ToolName != "Task" {
		t.Fatalf("tool starts = %#v", toolsStarted)
	}
	outcomes := map[provider.NativeToolCallID]provider.ToolOutcome{}
	for _, completed := range toolsCompleted {
		outcomes[completed.ToolCallID] = completed.Outcome
	}
	if outcomes["toolu_edit"] != provider.ToolSucceeded || outcomes["toolu_write"] != provider.ToolFailed || outcomes["toolu_task"] != provider.ToolSucceeded {
		t.Fatalf("tool outcomes = %#v", outcomes)
	}
	updates := bodiesOf[provider.ToolUpdatedEvent](events)
	if len(updates) != 1 || updates[0].ToolCallID != "toolu_task" || updates[0].Output.Text != "sub-agent looking" {
		t.Fatalf("sub-agent updates = %#v", updates)
	}

	usage := bodiesOf[provider.UsageEvent](events)
	if len(usage) != 1 {
		t.Fatalf("usage = %#v", usage)
	}
	counters := usage[0].Counters
	if counters.InputTokens.Value != 12 || counters.OutputTokens.Value != 5 || counters.CacheReadTokens.Value != 4 || counters.CacheWriteTokens.Value != 3 || counters.ReasoningTokens.Value != 2 || usage[0].Model.ModelID != "claude-test-model" {
		t.Fatalf("usage counters = %#v", usage[0])
	}
	candidates := bodiesOf[provider.ResultCandidateEvent](events)
	if len(candidates) != 1 || string(candidates[0].Candidate.Payload) != `{"status":"done"}` || candidates[0].Candidate.Source != provider.CandidateNativeStructured || !candidates[0].Candidate.Assignment.Present {
		t.Fatalf("candidates = %#v", candidates)
	}
	if len(warningCodes(events)) != 2 {
		t.Fatalf("unexpected warnings %q", warningCodes(events))
	}
}

func assertUpdate(t *testing.T, update provider.TextSegmentUpdate, item, segment string, mode provider.UpdateMode, version, offset uint64, text string) {
	t.Helper()
	if string(update.ItemID) != item || string(update.SegmentID) != segment || update.Mode != mode || update.Version != version || update.ByteOffset != offset || update.Text != text {
		t.Fatalf("update = %#v, want %s/%s %s v%d @%d %q", update, item, segment, mode, version, offset, text)
	}
}

func TestStructuredOutputCandidateRequiresObjectAndNegotiation(t *testing.T) {
	tests := []struct {
		name     string
		contract provider.ResultContractRef
		output   string
		warning  string
	}{
		{"array under native schema", schemaContract, `[1]`, "result_candidate_invalid_json"},
		{"string under native schema", schemaContract, `"done"`, "result_candidate_invalid_json"},
		{"object without negotiation", noneContract, `{"status":"done"}`, "result_candidate_not_eligible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeClaude(t)
			fake.onUser = func(f *fakeClaude, message map[string]any) {
				f.write(`{"type":"result","subtype":"success","is_error":false,"session_id":"` + f.nativeSession() + `","terminal_reason":"completed","structured_output":` + tt.output + `,"user_message_uuids":["` + messageUUID(message) + `"]}`)
			}
			s, stream := openTestSession(t, fake, tt.contract)
			startTurn(t, s, tt.contract)
			events := receiveUntil(t, stream, isTurnTerminal)
			if len(bodiesOf[provider.ResultCandidateEvent](events)) != 0 || !hasWarning(events, tt.warning) {
				t.Fatalf("events kinds/warnings = %q", warningCodes(events))
			}
		})
	}
}

// A steer preempts the request: claude reports an aborted_streaming result
// for the original input, then echoes the steer and ends the turn with a
// result naming both inputs. Only that last result ends the turn.
func TestSteerIntermediateResultDoesNotEndTurn(t *testing.T) {
	fake := newFakeClaude(t)
	var firstUUID string
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		if message["priority"] != "now" {
			firstUUID = messageUUID(message)
			echoAndStart(f, message)
			return
		}
		f.writeFixture("steer_intermediate.ndjson", map[string]string{"INPUT": firstUUID, "STEER": messageUUID(message)})
	}
	s, stream := openTestSession(t, fake, noneContract)
	started := startTurn(t, s, noneContract)
	receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnActive
	})
	next := testAssignment(2)
	receipt, err := s.Steer(context.Background(), provider.SteerRequest{
		RequestID: "steer", MessageID: "message", Session: s.Ref(), ExpectedActiveTurn: started.Turn,
		CurrentAssignment: testAssignment(1), ExpectedRevision: 1, NewAssignment: next,
		Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Kind: provider.InputInstruction, Text: "amend"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Disposition != provider.DeliveryTransportAccepted || receipt.Receipt != provider.ReceiptNone || receipt.DispatchedFor.Value != next {
		t.Fatalf("receipt = %#v", receipt)
	}
	events := receiveUntil(t, stream, isTurnTerminal)
	steerMessage := fake.messages("user")[1]
	if steerMessage["priority"] != "now" {
		t.Fatalf("steer message = %#v", steerMessage)
	}
	intermediate, accepted, terminals := -1, -1, 0
	for index, event := range events {
		switch body := event.Body().(type) {
		case provider.ProtocolWarningEvent:
			if body.Code == "native_intermediate_result" {
				intermediate = index
				if event.Envelope.Assignment.Present {
					t.Fatal("event during an unresolved steer carried attribution")
				}
			}
		case provider.ControlReceiptEvent:
			accepted = index
			if body.Receipt.Receipt != provider.ReceiptNativeKnown || string(body.Receipt.NativeReceiptID.Value) != messageUUID(steerMessage) {
				t.Fatalf("native receipt = %#v", body.Receipt)
			}
		case provider.TurnStateEvent:
			if body.Current == provider.TurnNativeTerminal {
				terminals++
				if event.Envelope.Assignment.Value != next {
					t.Fatalf("terminal attribution = %#v", event.Envelope.Assignment)
				}
			}
		}
	}
	if intermediate < 0 || accepted < intermediate || terminals != 1 {
		t.Fatalf("intermediate=%d accepted=%d terminals=%d warnings=%q", intermediate, accepted, terminals, warningCodes(events))
	}
	usage := bodiesOf[provider.UsageEvent](events)
	if len(usage) != 2 || usage[0].Measurement != provider.UsageMeasurementUnknown || usage[1].Measurement != provider.UsageCumulativeSnapshot {
		t.Fatalf("usage = %#v", usage)
	}
}

// A steer without delivery evidence leaves attribution unresolved past the
// turn's end, and blocks a new turn until native evidence resolves it.
func TestSteerWithoutEvidenceStaysUnresolved(t *testing.T) {
	fake := newFakeClaude(t)
	var firstUUID string
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		if message["priority"] != "now" {
			firstUUID = messageUUID(message)
			echoAndStart(f, message)
			return
		}
		f.write(resultLine(f, "completed", firstUUID))
	}
	s, stream := openTestSession(t, fake, noneContract)
	started := startTurn(t, s, noneContract)
	_, err := s.Steer(context.Background(), provider.SteerRequest{
		RequestID: "steer", Session: s.Ref(), ExpectedActiveTurn: started.Turn, CurrentAssignment: testAssignment(1),
		ExpectedRevision: 1, NewAssignment: testAssignment(2), Mode: provider.DeliverActiveTurn,
		Parts: []provider.InputPart{{Kind: provider.InputInstruction, Text: "amend"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		warning, ok := event.Body().(provider.ProtocolWarningEvent)
		return ok && warning.Code == "steer_delivery_unknown"
	})
	if events[len(events)-1].Envelope.Assignment.Present {
		t.Fatal("unresolved steer carried attribution")
	}
	_, err = s.StartTurn(context.Background(), provider.StartTurnRequest{Session: s.Ref(), Assignment: testAssignment(1), Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "next"}}, ResultContract: noneContract})
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorStaleBinding {
		t.Fatalf("start during unresolved steer err = %v", err)
	}
	// Late native evidence still resolves the correction.
	steerUUID := messageUUID(fake.messages("user")[1])
	fake.write(`{"type":"user","message":{"role":"user","content":"amend"},"session_id":"` + fake.nativeSession() + `","uuid":"` + steerUUID + `","isReplay":true}`)
	events = receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		_, ok := event.Body().(provider.ControlReceiptEvent)
		return ok
	})
	if !events[len(events)-1].Envelope.Assignment.Present || events[len(events)-1].Envelope.Assignment.Value.Revision != 2 {
		t.Fatalf("resolved attribution = %#v", events[len(events)-1].Envelope)
	}
}

func TestForeignResultDoesNotClearActiveTurn(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		echoAndStart(f, message)
		f.writeFixture("wake_result.ndjson", nil)
		f.write(resultLine(f, "completed", "00000000-0000-4000-8000-00000000ffff"))
	}
	s, stream := openTestSession(t, fake, noneContract)
	started := startTurn(t, s, noneContract)
	events := receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		warning, ok := event.Body().(provider.ProtocolWarningEvent)
		return ok && warning.Code == "uncorrelated_result"
	})
	wakes := 0
	for _, code := range warningCodes(events) {
		if code == "native_wake" {
			wakes++
		}
	}
	if wakes != 2 || len(bodiesOf[provider.TurnStateEvent](events)) != 1 {
		t.Fatalf("warnings %q turn states %#v", warningCodes(events), bodiesOf[provider.TurnStateEvent](events))
	}
	if snapshot := observe(t, s); snapshot.ActiveTurn.Value != started.Turn {
		t.Fatalf("foreign result cleared the active turn: %#v", snapshot)
	}
	fake.write(resultLine(fake, "completed", string(started.Native.Turn.Value)))
	receiveUntil(t, stream, isTurnTerminal)
	if snapshot := observe(t, s); snapshot.ActiveTurn.Present {
		t.Fatalf("correlated result did not end the turn: %#v", snapshot)
	}
}

func TestInterruptIsReceiptOnly(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = echoAndStart
	s, stream := openTestSession(t, fake, noneContract)
	started := startTurn(t, s, noneContract)
	result, err := s.InterruptTurn(context.Background(), provider.InterruptRequest{
		RequestID: "interrupt", Session: s.Ref(), ExpectedActiveTurn: started.Turn, Assignment: testAssignment(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StopOutcome != provider.StopUnknown || result.Quiescence != provider.QuiescenceUnknown {
		t.Fatalf("interrupt result = %#v", result)
	}
	if subtypes := fake.controlSubtypes(); subtypes[len(subtypes)-1] != "interrupt" {
		t.Fatalf("control requests = %q", subtypes)
	}
	receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnInterruptRequested
	})
	if snapshot := observe(t, s); !snapshot.ActiveTurn.Present {
		t.Fatal("interrupt receipt ended the turn")
	}
	fake.write(`{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":"` + fake.nativeSession() + `","terminal_reason":"interrupted","user_message_uuids":["` + string(started.Native.Turn.Value) + `"]}`)
	events := receiveUntil(t, stream, isTurnTerminal)
	if !hasWarning(events, "native_turn_error") || observe(t, s).ActiveTurn.Present {
		t.Fatalf("interrupted result warnings %q", warningCodes(events))
	}
	stale := provider.InterruptRequest{Session: s.Ref(), ExpectedActiveTurn: started.Turn, Assignment: testAssignment(1)}
	if _, err := s.InterruptTurn(context.Background(), stale); err == nil {
		t.Fatal("interrupt without an active turn succeeded")
	}
}

func TestCloseVariants(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = echoAndStart
	s, stream := openTestSession(t, fake, noneContract)
	startTurn(t, s, noneContract)
	for policy, code := range map[provider.ClosePolicy]provider.ErrorCode{
		provider.CloseExplicitDetach:   provider.ErrorCapabilityUnsupported,
		provider.CloseRequireQuiescent: provider.ErrorStaleBinding,
	} {
		_, err := s.Close(context.Background(), provider.CloseSessionRequest{Session: s.Ref(), Policy: policy})
		var providerErr *provider.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Code != code {
			t.Fatalf("%s err = %v, want %s", policy, err, code)
		}
	}
	result, err := s.Close(context.Background(), provider.CloseSessionRequest{Session: s.Ref(), Policy: provider.CloseInterruptThenClose})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HandleClosed || !result.ResourcesReleased || result.ExecutionState != provider.ExecutionNone {
		t.Fatalf("close result = %#v", result)
	}
	if subtypes := fake.controlSubtypes(); subtypes[len(subtypes)-1] != "interrupt" {
		t.Fatalf("interrupt_then_close did not interrupt: %q", subtypes)
	}
	events, err := receiveUntilQuiet(stream, 200*time.Millisecond)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("stream terminal = %v, want EOF after close", err)
	}
	states := bodiesOf[provider.SessionStateEvent](events)
	if len(states) == 0 || states[len(states)-1].Current != provider.SessionClosed {
		t.Fatalf("session states = %#v", states)
	}
	for _, state := range states {
		if state.Current == provider.SessionLost {
			t.Fatal("close was reported as session loss")
		}
	}
	for _, event := range events {
		if isTurnTerminal(event) {
			t.Fatal("close invented a native terminal turn event")
		}
	}
	if _, err := s.Close(context.Background(), provider.CloseSessionRequest{Session: s.Ref(), Policy: provider.CloseInterruptThenClose}); err == nil {
		t.Fatal("second close succeeded")
	}
}

func TestCloseIdleRequireQuiescentClosesStdin(t *testing.T) {
	fake := newFakeClaude(t)
	s, _ := openTestSession(t, fake, noneContract)
	result, err := s.Close(context.Background(), provider.CloseSessionRequest{Session: s.Ref(), Policy: provider.CloseRequireQuiescent})
	if err != nil || !result.ResourcesReleased {
		t.Fatalf("close = %#v, %v", result, err)
	}
	if !fake.process().exited() {
		t.Fatal("owned process did not exit")
	}
	for _, subtype := range fake.controlSubtypes() {
		if subtype == "interrupt" {
			t.Fatal("idle close sent an interrupt")
		}
	}
}

func TestUnsupportedOperationsAreTyped(t *testing.T) {
	fake := newFakeClaude(t)
	s, _ := openTestSession(t, fake, noneContract)
	_, permissionErr := s.RespondPermission(context.Background(), provider.PermissionResponse{})
	_, questionErr := s.AnswerQuestion(context.Background(), provider.QuestionAnswer{})
	_, replayErr := s.Events(context.Background(), provider.EventCursor{AdapterCursor: "1"})
	for name, err := range map[string]error{"permission": permissionErr, "question": questionErr, "replay": replayErr} {
		var providerErr *provider.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCapabilityUnsupported || providerErr.RemoteEffect != provider.RemoteEffectNotSent {
			t.Fatalf("%s err = %v", name, err)
		}
	}
	if _, ok := any(New(Config{})).(provider.ResumableProvider); ok {
		t.Fatal("adapter must not implement resume")
	}
}

func TestSteerAndStartTurnEligibility(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = echoAndStart
	s, _ := openTestSession(t, fake, noneContract)
	steer := provider.SteerRequest{Session: s.Ref(), CurrentAssignment: testAssignment(1), ExpectedRevision: 1, NewAssignment: testAssignment(2), Mode: provider.DeliverActiveTurn}
	if _, err := s.Steer(context.Background(), steer); err == nil {
		t.Fatal("steer without an active turn succeeded")
	}
	started := startTurn(t, s, noneContract)
	if _, err := s.StartTurn(context.Background(), provider.StartTurnRequest{Session: s.Ref(), Assignment: testAssignment(1), ResultContract: noneContract}); err == nil {
		t.Fatal("second concurrent turn started")
	}
	steer.ExpectedActiveTurn = started.Turn
	steer.Mode = provider.DeliverNextBoundary
	var providerErr *provider.ProviderError
	if _, err := s.Steer(context.Background(), steer); !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCapabilityUnsupported {
		t.Fatalf("next-boundary steer err = %v", err)
	}
	steer.Mode = provider.DeliverActiveTurn
	steer.ExpectedRevision = 7
	if _, err := s.Steer(context.Background(), steer); !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorStaleBinding {
		t.Fatalf("stale revision steer err = %v", err)
	}
	// The started turn's message is recorded asynchronously by the fake.
	deadline := time.Now().Add(time.Second)
	for len(fake.messages("user")) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if got := len(fake.messages("user")); got != 1 {
		t.Fatalf("refused steers were dispatched: %d user messages", got)
	}
}

func TestStartTurnRejectsChangedContract(t *testing.T) {
	fake := newFakeClaude(t)
	s, _ := openTestSession(t, fake, noneContract)
	_, err := s.StartTurn(context.Background(), provider.StartTurnRequest{Session: s.Ref(), Assignment: testAssignment(1), ResultContract: schemaContract})
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorProtocolViolation {
		t.Fatalf("err = %v", err)
	}
}

func TestStartTurnCancelledBeforeDispatchReleasesLease(t *testing.T) {
	fake := newFakeClaude(t)
	s, _ := openTestSession(t, fake, noneContract)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.StartTurn(ctx, provider.StartTurnRequest{Session: s.Ref(), Assignment: testAssignment(1), ResultContract: noneContract})
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCallerCancelled || providerErr.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("err = %v", err)
	}
	startTurn(t, s, noneContract)
}
