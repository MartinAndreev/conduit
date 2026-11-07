package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func openFixture(t *testing.T, name string) (*testHarness, chan error) {
	t.Helper()
	failures := make(chan error, 1)
	return openHarness(t, replayAgent(loadFixture(t, name), failures), nil), failures
}

func TestFixtureTurnEndsWithEndTurn(t *testing.T) {
	h, failures := openFixture(t, "turn_end_turn.ndjson")
	if version := h.session.Capabilities().HarnessVersion; version != "0.0.14" {
		t.Fatalf("HarnessVersion = %q", version)
	}
	started, err := h.startTurn(context.Background(), "Write a short note.")
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if started.TargetingStrength != provider.TargetSessionLease || started.ResultEnforcement != provider.SchemaNone {
		t.Fatalf("start result = %+v", started)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatalf("collect: %v\n%s", err, describe(events))
	}

	turns := bodiesOf[provider.TurnStateEvent](events)
	if len(turns) != 2 || turns[0].Current != provider.TurnActive || turns[1].Current != provider.TurnNativeTerminal || turns[1].TerminalReason != "end_turn" {
		t.Fatalf("turn states = %+v", turns)
	}
	thoughts := bodiesOf[provider.ReasoningSummaryEvent](events)
	if len(thoughts) != 2 || thoughts[1].Update.ItemID != "message:msg-thought-1" || thoughts[1].Update.Version != 2 ||
		thoughts[1].Update.ByteOffset != uint64(len("Planning the ")) || !thoughts[1].Update.BaseVersion.Present {
		t.Fatalf("reasoning = %+v", thoughts)
	}
	texts := bodiesOf[provider.AssistantTextEvent](events)
	if len(texts) != 2 || texts[0].Update.Text+texts[1].Update.Text != "Note written." || texts[1].Update.Mode != provider.UpdateDelta {
		t.Fatalf("text = %+v", texts)
	}
	toolStarts := bodiesOf[provider.ToolStartedEvent](events)
	toolDone := bodiesOf[provider.ToolCompletedEvent](events)
	if len(toolStarts) != 2 || toolStarts[0].ToolCallID != "call-edit-1" || toolStarts[0].ToolName != "Writing" ||
		!strings.Contains(toolStarts[0].Arguments.Text, "notes.txt") {
		t.Fatalf("tool starts = %+v", toolStarts)
	}
	if len(toolDone) != 2 || toolDone[0].Outcome != provider.ToolSucceeded || toolDone[0].Output.Text != "wrote notes.txt (5 bytes)" ||
		toolDone[1].ToolCallID != "call-sub-1" {
		t.Fatalf("tool completions = %+v", toolDone)
	}
	files := bodiesOf[provider.FileActivityEvent](events)
	if len(files) != 1 || files[0].Operation != provider.FileUpdate || files[0].NativePath != "notes.txt" || files[0].ToolCallID.Value != "call-edit-1" {
		t.Fatalf("file activity = %+v", files)
	}
	usage := bodiesOf[provider.UsageEvent](events)
	if len(usage) != 2 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage[0].Scope != provider.UsageOther || usage[0].Counters.InputTokens.Present {
		t.Fatalf("usage_update must keep counters unknown: %+v", usage[0])
	}
	turnUsage := usage[1]
	if turnUsage.Scope != provider.UsagePrimaryTurn || turnUsage.ScopeID != string(started.Turn.AdapterTurnID) ||
		turnUsage.Measurement != provider.UsageCumulativeSnapshot || turnUsage.Counters.InputTokens.Value != 21465 ||
		turnUsage.Counters.ReasoningTokens.Value != 86 || turnUsage.Counters.CacheReadTokens.Present {
		t.Fatalf("turn usage = %+v", turnUsage)
	}
	for _, event := range events {
		if body, ok := event.Body().(provider.ProtocolWarningEvent); ok {
			t.Fatalf("unexpected warning %+v", body)
		}
		if event.Envelope.Turn.Present && (event.Envelope.Native.Session.Value != "fx-session-01" || event.Envelope.Assignment.Value != testAssignment) {
			t.Fatalf("envelope = %+v", event.Envelope)
		}
	}
	terminal := events[len(events)-1]
	if terminal.Envelope.Turn.Value != started.Turn {
		t.Fatalf("terminal turn = %+v", terminal.Envelope.Turn)
	}

	result, err := h.session.Close(context.Background(), provider.CloseSessionRequest{Session: h.session.Ref(), Policy: provider.CloseRequireQuiescent})
	if err != nil || !result.HandleClosed || !result.ResourcesReleased || result.ExecutionState != provider.ExecutionNone {
		t.Fatalf("Close = %+v, %v", result, err)
	}
	awaitReplay(t, failures)
	if !h.launcher.process(t).reaped() {
		t.Fatal("agent process was not reaped")
	}
}

func TestFixtureInterruptEndsWithCancelledResponse(t *testing.T) {
	h, failures := openFixture(t, "turn_cancelled.ndjson")
	started, err := h.startTurn(context.Background(), "Run a long task.")
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	interrupted, err := h.session.InterruptTurn(context.Background(), provider.InterruptRequest{
		RequestID: "interrupt-1", Session: h.session.Ref(), ExpectedActiveTurn: started.Turn, Assignment: testAssignment,
		Requirement: provider.InterruptBestEffort,
	})
	if err != nil {
		t.Fatalf("InterruptTurn: %v", err)
	}
	if interrupted.StopOutcome != provider.StopUnknown || interrupted.Quiescence != provider.QuiescenceUnknown {
		t.Fatalf("interrupt result = %+v", interrupted)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	turns := bodiesOf[provider.TurnStateEvent](events)
	last := turns[len(turns)-1]
	// The cancelled reply may race the interrupt receipt, so the terminal can
	// follow either the active or the interrupt-requested state.
	if (last.Previous != provider.TurnInterruptRequested && last.Previous != provider.TurnActive) || last.Current != provider.TurnNativeTerminal || last.TerminalReason != "cancelled" {
		t.Fatalf("turn states = %+v", turns)
	}
	if usage := bodiesOf[provider.UsageEvent](events); len(usage) != 0 {
		t.Fatalf("empty native usage must not emit usage: %+v", usage)
	}
	awaitReplay(t, failures)
	h.awaitTurnReleased()
	snapshot, err := h.session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: h.session.Ref()})
	if err != nil || snapshot.ActiveTurn.Present || snapshot.SessionState != provider.SessionOpen {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
}

func TestFixturePermissionIsRejectedOnce(t *testing.T) {
	h, failures := openFixture(t, "permission_reject_once.ndjson")
	if _, err := h.startTurn(context.Background(), "Write a file."); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	awaitReplay(t, failures)
	if len(warnings(events, "permission_auto_rejected")) != 1 {
		t.Fatalf("missing permission warning:\n%s", describe(events))
	}
	done := bodiesOf[provider.ToolCompletedEvent](events)
	if len(done) != 1 || done[0].Outcome != provider.ToolFailed {
		t.Fatalf("tool completion = %+v", done)
	}
	if files := bodiesOf[provider.FileActivityEvent](events); len(files) != 0 {
		t.Fatalf("denied edit reported file activity: %+v", files)
	}
	if requested := bodiesOf[provider.PermissionRequestedEvent](events); len(requested) != 0 {
		t.Fatalf("auto-answered permission must not ask the caller: %+v", requested)
	}
}

func TestFixtureElicitationIsDeclined(t *testing.T) {
	h, failures := openFixture(t, "elicitation_decline.ndjson")
	if _, err := h.startTurn(context.Background(), "Ask me something."); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	awaitReplay(t, failures)
	if len(warnings(events, "elicitation_declined")) != 1 {
		t.Fatalf("missing elicitation warning:\n%s", describe(events))
	}
	texts := bodiesOf[provider.AssistantTextEvent](events)
	if len(texts) != 1 || !strings.HasPrefix(string(texts[0].Update.ItemID), "turn:") {
		t.Fatalf("text without messageId = %+v", texts)
	}
}

func TestFixturePromptErrorFailsTurn(t *testing.T) {
	h, failures := openFixture(t, "prompt_error.ndjson")
	if _, err := h.startTurn(context.Background(), "Fail please."); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	events, err := h.collectUntil(isTurnTerminal)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	awaitReplay(t, failures)
	failed := warnings(events, "native_turn_error")
	if len(failed) != 1 || !strings.Contains(failed[0].Detail.Text, "invalid request") {
		t.Fatalf("native_turn_error = %+v", failed)
	}
	turns := bodiesOf[provider.TurnStateEvent](events)
	if last := turns[len(turns)-1]; last.Current != provider.TurnNativeTerminal || last.TerminalReason != "error" {
		t.Fatalf("turn states = %+v", turns)
	}
	// The session stays usable after a failed turn.
	h.awaitTurnReleased()
	snapshot, _ := h.session.ObserveState(context.Background(), provider.ObserveStateRequest{Session: h.session.Ref()})
	if snapshot.SessionState != provider.SessionOpen || snapshot.ActiveTurn.Present {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	// session/close is not advertised by this agent.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := h.session.Close(ctx, provider.CloseSessionRequest{Session: h.session.Ref(), Policy: provider.CloseRequireQuiescent}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if frames := h.launcher.framesFor("session/close"); len(frames) != 0 {
		t.Fatalf("session/close sent without advertisement: %+v", frames)
	}
}

func TestFixtureInitializeAdvertisesNoClientFilesystemOrTerminal(t *testing.T) {
	h, failures := openFixture(t, "turn_cancelled.ndjson")
	frames := h.launcher.framesFor("initialize")
	if len(frames) != 1 {
		t.Fatalf("initialize frames = %d", len(frames))
	}
	var params struct {
		ClientCapabilities map[string]json.RawMessage `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(frames[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if string(params.ClientCapabilities["terminal"]) != "false" || !strings.Contains(string(params.ClientCapabilities["fs"]), `"writeTextFile":false`) {
		t.Fatalf("client capabilities = %s", frames[0].Params)
	}
	_ = failures
}
