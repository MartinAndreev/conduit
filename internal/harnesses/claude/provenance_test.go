package claude

import (
	"context"
	"testing"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func assistantTextLine(f *fakeClaude, id, text string) string {
	return `{"type":"assistant","message":{"model":"claude-test-model","id":"` + id + `","type":"message","role":"assistant","content":[{"type":"text","text":"` + text + `"}]},"parent_tool_use_id":null,"session_id":"` + f.nativeSession() + `","uuid":"00000000-0000-4000-8000-0000000009` + id[len(id)-2:] + `"}`
}

func startedAndEcho(f *fakeClaude, uuid string) {
	f.write(`{"type":"command_lifecycle","command_uuid":"` + uuid + `","state":"started","session_id":"` + f.nativeSession() + `"}`)
	f.write(`{"type":"user","message":{"role":"user","content":"x"},"session_id":"` + f.nativeSession() + `","parent_tool_use_id":null,"uuid":"` + uuid + `","isReplay":true}`)
}

func assistantTextEvents(events []provider.ProviderEvent, text string) []provider.ProviderEvent {
	var result []provider.ProviderEvent
	for _, event := range events {
		if body, ok := event.Body().(provider.AssistantTextEvent); ok && body.Update.Text == text {
			result = append(result, event)
		}
	}
	return result
}

// After an explicit release, a released steer still queued in claude may run
// before the new turn's input. Its lifecycle, text and result stay without
// turn or assignment attribution; the new turn's own output is attributed.
func TestReleasedSteerLateOutputStaysUnattributed(t *testing.T) {
	s, stream, fake := steerEndingTurn(t, nil, func(f *fakeClaude, input string) string { return queuedResultLine(f, "1", input) })
	receiveUntil(t, stream, isWarning("steer_delivery_unknown"))
	steerUUID := messageUUID(fake.messages("user")[1])
	newInput := make(chan string, 1)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		// claude queues the new input behind the released steer.
		f.write(`{"type":"command_lifecycle","command_uuid":"` + messageUUID(message) + `","state":"queued","session_id":"` + f.nativeSession() + `"}`)
		newInput <- messageUUID(message)
	}
	started, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.Some(provider.MessageID("steer-message"))))
	if err != nil {
		t.Fatal(err)
	}
	newUUID := <-newInput

	startedAndEcho(fake, steerUUID)
	fake.write(assistantTextLine(fake, "msg_late01", "late"))
	// An intermediate result does not end the released steer's native work.
	fake.write(`{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":"` + fake.nativeSession() + `","terminal_reason":"aborted_streaming","queued_turn_count":1,"user_message_uuids":["` + steerUUID + `"]}`)
	fake.write(assistantTextLine(fake, "msg_late03", "later"))
	fake.write(queuedResultLine(fake, "1", steerUUID))
	startedAndEcho(fake, newUUID)
	fake.write(assistantTextLine(fake, "msg_mine02", "mine"))
	fake.write(queuedResultLine(fake, "0", newUUID))
	events := receiveUntil(t, stream, isTurnTerminal)

	late := append(assistantTextEvents(events, "late"), assistantTextEvents(events, "later")...)
	if len(late) != 2 {
		t.Fatal("released steer output was not normalized")
	}
	for _, event := range late {
		if event.Envelope.Assignment.Present || event.Envelope.Turn.Present {
			t.Fatalf("released steer output attributed: %#v", event.Envelope)
		}
	}
	for _, event := range events {
		if warning, ok := event.Body().(provider.ProtocolWarningEvent); ok && warning.Code == "uncorrelated_result" && event.Envelope.Assignment.Present {
			t.Fatalf("released steer result attributed: %#v", event.Envelope)
		}
		if _, ok := event.Body().(provider.ControlReceiptEvent); ok {
			t.Fatal("released steer was relabelled by late evidence")
		}
	}
	mine := assistantTextEvents(events, "mine")
	if len(mine) == 0 {
		t.Fatal("new turn output was not normalized")
	}
	for _, event := range mine {
		if !event.Envelope.Assignment.Present || event.Envelope.Assignment.Value != testAssignment(1) || event.Envelope.Turn.Value != started.Turn {
			t.Fatalf("new turn output attribution = %#v", event.Envelope)
		}
	}
	terminal := events[len(events)-1]
	if !terminal.Envelope.Assignment.Present || terminal.Envelope.Turn.Value != started.Turn {
		t.Fatalf("new turn terminal attribution = %#v", terminal.Envelope)
	}
}

// A terminal result read between the steer's reservation and its dispatch
// claim withdraws the steer: it is never written, Steer fails not-sent, and
// the session is not blocked.
func TestSteerReservedAtTerminalResultIsWithdrawn(t *testing.T) {
	fake := newFakeClaude(t)
	var firstUUID string
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		firstUUID = messageUUID(message)
		echoAndStart(f, message)
	}
	opened, stream := openTestSession(t, fake, noneContract)
	s := opened.(*session)
	turn := startTurn(t, s, noneContract)
	receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnActive
	})
	entered, release := make(chan struct{}), make(chan struct{})
	s.afterSteerReserved = func() {
		close(entered)
		<-release
	}
	steerErr := make(chan error, 1)
	go func() {
		_, err := s.Steer(context.Background(), provider.SteerRequest{
			RequestID: "steer", MessageID: "steer-message", Session: s.Ref(), ExpectedActiveTurn: turn.Turn,
			CurrentAssignment: testAssignment(1), ExpectedRevision: 1, NewAssignment: testAssignment(2),
			Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Kind: provider.InputInstruction, Text: "amend"}},
		})
		steerErr <- err
	}()
	<-entered
	fake.write(queuedResultLine(fake, "0", firstUUID))
	events := receiveUntil(t, stream, isWarning("steer_withdrawn"))
	if hasWarning(events, "steer_not_observed") {
		t.Fatal("a never-written steer was judged not observed")
	}
	close(release)
	requireStale(t, <-steerErr, "withdrawn steer")
	for _, message := range fake.messages("user") {
		if message["priority"] == "now" {
			t.Fatal("withdrawn steer was written")
		}
	}
	if _, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.None[provider.MessageID]())); err != nil {
		t.Fatalf("start after withdrawal: %v", err)
	}
}
