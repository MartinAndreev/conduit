package claude

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func assertSessionLost(t *testing.T, s provider.Session, stream provider.EventStream) []provider.ProviderEvent {
	t.Helper()
	events, err := receiveUntilQuiet(stream, 2*time.Second)
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorSessionLost {
		t.Fatalf("stream terminal = %v, want session_lost", err)
	}
	if snapshot := observe(t, s); snapshot.SessionState != provider.SessionLost {
		t.Fatalf("session state = %s, want lost", snapshot.SessionState)
	}
	for _, turn := range bodiesOf[provider.TurnStateEvent](events) {
		if turn.Current == provider.TurnNativeTerminal {
			t.Fatal("session loss invented a native terminal turn event")
		}
	}
	return events
}

// startTurnRacingLoss starts a turn whose fake reply loses the session at once.
// The loss may beat the completion of the StartTurn write; then the dispatch
// is correctly uncertain (delivery_unknown, possibly sent) or the session is
// already lost.
func startTurnRacingLoss(t *testing.T, s provider.Session) {
	t.Helper()
	_, err := s.StartTurn(context.Background(), provider.StartTurnRequest{
		RequestID: "start", Session: s.Ref(), Assignment: testAssignment(1),
		Input: []provider.InputPart{{Kind: provider.InputInstruction, Text: "do work"}}, ResultContract: noneContract,
	})
	if err == nil {
		return
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("start err = %v", err)
	}
	switch providerErr.Code {
	case provider.ErrorDeliveryUnknown:
		if providerErr.RemoteEffect != provider.RemoteEffectPossiblySent {
			t.Fatalf("delivery_unknown remote effect = %s", providerErr.RemoteEffect)
		}
	case provider.ErrorSessionLost:
	default:
		t.Fatalf("start err = %v, want success, delivery_unknown or session_lost", err)
	}
}

// A native line above MaxLineBytes ends the session visibly and stops the
// owned process instead of leaving a silent open session.
func TestLineBoundLosesSession(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		echoAndStart(f, message)
		f.write(`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"` + strings.Repeat("a", 4096) + `"}]}}`)
	}
	config := testConfig(fake)
	config.MaxLineBytes = 1024
	opened, err := New(config).OpenSession(context.Background(), openRequest(t, noneContract))
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := opened.Session.Events(context.Background(), provider.EventCursor{})
	startTurnRacingLoss(t, opened.Session)
	assertSessionLost(t, opened.Session, stream)
	if !fake.process().killed.Load() {
		t.Fatal("owned process was not killed after transport loss")
	}
}

// The default line bound admits a large single message.
func TestDefaultLineBoundAcceptsLargeMessage(t *testing.T) {
	huge := strings.Repeat("a", 5*1024*1024)
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		echoAndStart(f, message)
		f.write(`{"type":"assistant","message":{"id":"m","model":"claude-test-model","content":[{"type":"text","text":"` + huge + `"}]},"parent_tool_use_id":null}`)
	}
	s, stream := openTestSession(t, fake, noneContract)
	startTurn(t, s, noneContract)
	events := receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		_, ok := event.Body().(provider.AssistantTextEvent)
		return ok
	})
	text := bodiesOf[provider.AssistantTextEvent](events)[0]
	if !text.Update.Truncated || len(text.Update.Text) != maxNativeTextBytes || text.Update.OriginalBytes.Value != uint64(len(huge)) {
		t.Fatalf("large text not bounded: truncated=%v len=%d", text.Update.Truncated, len(text.Update.Text))
	}
}

func TestStdoutEOFLosesSessionWithoutInventedResult(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = echoAndStart
	s, stream := openTestSession(t, fake, noneContract)
	startTurn(t, s, noneContract)
	receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnActive
	})
	_ = fake.process().serverOutput.Close()
	assertSessionLost(t, s, stream)
	if _, err := s.StartTurn(context.Background(), provider.StartTurnRequest{Session: s.Ref(), Assignment: testAssignment(1), ResultContract: noneContract}); err == nil {
		t.Fatal("turn started on a lost session")
	}
}

func TestMalformedLineLosesSession(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, _ map[string]any) { f.write(`{"type":"assistant",`) }
	s, stream := openTestSession(t, fake, noneContract)
	startTurnRacingLoss(t, s)
	assertSessionLost(t, s, stream)
}

func TestProcessExitLosesSession(t *testing.T) {
	fake := newFakeClaude(t)
	s, stream := openTestSession(t, fake, noneContract)
	fake.process().exit()
	assertSessionLost(t, s, stream)
}

func TestNativeControlRequestIsRejected(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, _ map[string]any) {
		f.write(`{"type":"control_request","request_id":"native-1","request":{"subtype":"can_use_tool","tool_name":"Bash"}}`)
	}
	s, stream := openTestSession(t, fake, noneContract)
	startTurn(t, s, noneContract)
	receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		warning, ok := event.Body().(provider.ProtocolWarningEvent)
		return ok && warning.Code == "unsupported_control_request"
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, message := range fake.messages("control_response") {
			response, _ := message["response"].(map[string]any)
			if response["request_id"] == "native-1" && response["subtype"] == "error" {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("native control request was not answered")
}

func TestModelChangeAndUnknownMessagesWarn(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		echoAndStart(f, message)
		f.write(`{"type":"system","subtype":"init","model":"claude-test-model"}`)
		f.write(`{"type":"system","subtype":"init","model":"claude-other-model"}`)
		f.write(`{"type":"mystery"}`)
		f.write(`{"type":"mystery"}`)
		f.write(`{"type":"system","subtype":"status","status":"requesting"}`)
	}
	s, stream := openTestSession(t, fake, noneContract)
	startTurn(t, s, noneContract)
	events := receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.SessionStateEvent)
		return ok && body.NativeStatus == "requesting"
	})
	unknown := 0
	for _, code := range warningCodes(events) {
		if code == "unknown_native_message" {
			unknown++
		}
	}
	if !hasWarning(events, "model_changed") || unknown != 1 {
		t.Fatalf("warnings = %q", warningCodes(events))
	}
}

// A consumer that falls behind ends the stream with backpressure_exceeded.
func TestEventBufferOverflowFailsVisibly(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		echoAndStart(f, message)
		for range 20 {
			f.write(`{"type":"assistant","message":{"id":"m","model":"claude-test-model","content":[{"type":"text","text":"x"}]},"parent_tool_use_id":null}`)
		}
	}
	config := testConfig(fake)
	config.EventBuffer = 4
	opened, err := New(config).OpenSession(context.Background(), openRequest(t, noneContract))
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := opened.Session.Events(context.Background(), provider.EventCursor{})
	startTurn(t, opened.Session, noneContract)
	time.Sleep(100 * time.Millisecond)
	_, err = receiveUntilQuiet(stream, time.Second)
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorBackpressureExceeded {
		t.Fatalf("stream terminal = %v, want backpressure_exceeded", err)
	}
}
