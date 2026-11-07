package claude

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/claude/rules"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// queuedResultLine is a terminal result with an explicit queued_turn_count.
func queuedResultLine(f *fakeClaude, queued string, uuids ...string) string {
	return `{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"` + f.nativeSession() + `","terminal_reason":"completed","queued_turn_count":` + queued + `,"user_message_uuids":["` + strings.Join(uuids, `","`) + `"]}`
}

// steerEndingTurn opens a session, starts a turn and steers it; claude ends
// the turn without steer evidence by writing beforeResult (optional native
// lines for the steer UUID) and then the given result line.
func steerEndingTurn(t *testing.T, beforeResult func(f *fakeClaude, steerUUID string), result func(f *fakeClaude, inputUUID string) string) (provider.Session, provider.EventStream, *fakeClaude) {
	t.Helper()
	fake := newFakeClaude(t)
	var firstUUID string
	var adapter *session
	fake.onUser = func(f *fakeClaude, message map[string]any) {
		if message["priority"] != "now" {
			firstUUID = messageUUID(message)
			echoAndStart(f, message)
			return
		}
		if beforeResult != nil {
			beforeResult(f, messageUUID(message))
		}
		// The adapter judges "not observed" only for a steer whose write it
		// recorded as completed before reading the result, as with a real
		// result that follows the write.
		waitSteerWritten(t, adapter)
		f.write(result(f, firstUUID))
	}
	s, stream := openTestSession(t, fake, noneContract)
	adapter = s.(*session)
	started := startTurn(t, s, noneContract)
	_, err := s.Steer(context.Background(), provider.SteerRequest{
		RequestID: "steer", MessageID: "steer-message", Session: s.Ref(), ExpectedActiveTurn: started.Turn,
		CurrentAssignment: testAssignment(1), ExpectedRevision: 1, NewAssignment: testAssignment(2),
		Mode: provider.DeliverActiveTurn, Parts: []provider.InputPart{{Kind: provider.InputInstruction, Text: "amend"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, stream, fake
}

// waitSteerWritten polls, with a deadline, until the pending steer's write is
// recorded as complete (or the steer is no longer pending).
func waitSteerWritten(t *testing.T, s *session) {
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(time.Millisecond) {
		written := false
		_ = lockscope.WithMutexLock(&s.mu, func() error {
			written = s.steer == nil || s.steer.dispatch == rules.SteerWritten
			return nil
		})
		if written {
			return
		}
		if time.Now().After(deadline) {
			t.Error("steer write was never recorded")
			return
		}
	}
}

// writeBarrier writes a native line of a unique unknown kind and waits for its
// warning: the reader handles lines in order, so every event caused by an
// earlier line has been received once this returns.
func writeBarrier(t *testing.T, fake *fakeClaude, stream provider.EventStream, kind string) []provider.ProviderEvent {
	t.Helper()
	fake.write(`{"type":"` + kind + `"}`)
	return receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		warning, ok := event.Body().(provider.ProtocolWarningEvent)
		return ok && warning.Code == "unknown_native_message" && warning.OffendingNativeKind == kind
	})
}

func nextTurnRequest(s provider.Session, release provider.Optional[provider.MessageID]) provider.StartTurnRequest {
	return provider.StartTurnRequest{
		RequestID: "next", Session: s.Ref(), Assignment: testAssignment(1),
		Input:          []provider.InputPart{{Kind: provider.InputInstruction, Text: "next"}},
		ResultContract: noneContract, ReleaseUnresolvedSteer: release,
	}
}

func isWarning(code string) func(provider.ProviderEvent) bool {
	return func(event provider.ProviderEvent) bool {
		warning, ok := event.Body().(provider.ProtocolWarningEvent)
		return ok && warning.Code == code
	}
}

func requireStale(t *testing.T, err error, context string) {
	t.Helper()
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorStaleBinding || providerErr.RemoteEffect != provider.RemoteEffectNotSent {
		t.Fatalf("%s: err = %v, want stale_binding not sent", context, err)
	}
}

// Layer 1: no echo, no lifecycle for the steer and queued_turn_count 0 prove
// the steer was not observed. The binding is released without advancing the
// assignment; the turn's own output stays unattributed.
func TestSteerNotObservedAtTerminalResultReleasesBinding(t *testing.T) {
	s, stream, fake := steerEndingTurn(t, nil, func(f *fakeClaude, input string) string { return queuedResultLine(f, "0", input) })
	events := receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		_, ok := event.Body().(provider.ControlReceiptEvent)
		return ok
	})
	if hasWarning(events, "steer_delivery_unknown") || !hasWarning(events, "steer_not_observed") {
		t.Fatalf("warnings = %q", warningCodes(events))
	}
	for _, event := range events {
		if isTurnTerminal(event) && event.Envelope.Assignment.Present {
			t.Fatal("output of the turn with an unresolved steer carried attribution")
		}
	}
	receipt := events[len(events)-1].Body().(provider.ControlReceiptEvent).Receipt
	if receipt.MessageID != "steer-message" || receipt.RequestID != "steer" || receipt.Disposition != provider.DeliveryRejected ||
		receipt.Receipt != provider.ReceiptNone || receipt.DispatchedFor.Present || receipt.NativeReceiptID.Present {
		t.Fatalf("not-observed receipt = %#v", receipt)
	}
	if got := events[len(events)-1].Envelope.Assignment; !got.Present || got.Value != testAssignment(1) {
		t.Fatalf("attribution after release = %#v, want revision 1", got)
	}
	if _, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.None[provider.MessageID]())); err != nil {
		t.Fatalf("start after not-observed release: %v", err)
	}
	// A contradicting late echo of the released steer cannot relabel anything.
	steerUUID := messageUUID(fake.messages("user")[1])
	fake.write(`{"type":"user","message":{"role":"user","content":"amend"},"session_id":"` + fake.nativeSession() + `","uuid":"` + steerUUID + `","isReplay":true}`)
	late := writeBarrier(t, fake, stream, "conduit_test_barrier_late_echo")
	if receipts := bodiesOf[provider.ControlReceiptEvent](late); len(receipts) != 0 {
		t.Fatalf("late echo after release produced receipts %#v", receipts)
	}
}

// Queued work, an absent count, or a lifecycle for the steer keep the doubt:
// late evidence may still come, and StartTurn without a release is refused.
func TestSteerDoubtRemainsWhenClaudeMayStillObserveIt(t *testing.T) {
	cases := map[string]struct {
		before func(f *fakeClaude, steerUUID string)
		result func(f *fakeClaude, input string) string
	}{
		"queued turns": {nil, func(f *fakeClaude, input string) string { return queuedResultLine(f, "1", input) }},
		"count absent": {nil, func(f *fakeClaude, input string) string { return resultLine(f, "completed", input) }},
		"lifecycle queued": {
			func(f *fakeClaude, steerUUID string) {
				f.write(`{"type":"command_lifecycle","command_uuid":"` + steerUUID + `","state":"queued","session_id":"` + f.nativeSession() + `"}`)
			},
			func(f *fakeClaude, input string) string { return queuedResultLine(f, "0", input) },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, stream, fake := steerEndingTurn(t, tc.before, tc.result)
			events := receiveUntil(t, stream, isWarning("steer_delivery_unknown"))
			if hasWarning(events, "steer_not_observed") || len(bodiesOf[provider.ControlReceiptEvent](events)) != 0 {
				t.Fatalf("doubt was resolved: %q", warningCodes(events))
			}
			_, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.None[provider.MessageID]()))
			requireStale(t, err, "start during unresolved steer")
			// Late evidence still resolves the correction and advances it.
			steerUUID := messageUUID(fake.messages("user")[1])
			fake.write(`{"type":"command_lifecycle","command_uuid":"` + steerUUID + `","state":"started","session_id":"` + fake.nativeSession() + `"}`)
			events = receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
				_, ok := event.Body().(provider.ControlReceiptEvent)
				return ok
			})
			receipt := events[len(events)-1]
			if body := receipt.Body().(provider.ControlReceiptEvent).Receipt; body.Disposition != provider.DeliveryTransportAccepted || body.DispatchedFor.Value != testAssignment(2) {
				t.Fatalf("late receipt = %#v", body)
			}
			if !receipt.Envelope.Assignment.Present || receipt.Envelope.Assignment.Value.Revision != 2 {
				t.Fatalf("late attribution = %#v", receipt.Envelope.Assignment)
			}
		})
	}
}

// Layer 2: an explicit release naming exactly the unresolved steer starts the
// turn without adopting the steer's revision; late evidence for the released
// steer is ignored.
func TestStartTurnExplicitReleaseOfUnresolvedSteer(t *testing.T) {
	s, stream, fake := steerEndingTurn(t, nil, func(f *fakeClaude, input string) string { return queuedResultLine(f, "1", input) })
	receiveUntil(t, stream, isWarning("steer_delivery_unknown"))
	started, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.Some(provider.MessageID("steer-message"))))
	if err != nil {
		t.Fatalf("release start: %v", err)
	}
	if started.Assignment != testAssignment(1) {
		t.Fatalf("started assignment = %#v", started.Assignment)
	}
	events := receiveUntil(t, stream, func(event provider.ProviderEvent) bool {
		body, ok := event.Body().(provider.TurnStateEvent)
		return ok && body.Current == provider.TurnActive
	})
	if got := events[len(events)-1].Envelope.Assignment; !got.Present || got.Value != testAssignment(1) {
		t.Fatalf("new turn attribution = %#v", got)
	}
	steerUUID := messageUUID(fake.messages("user")[1])
	fake.write(`{"type":"command_lifecycle","command_uuid":"` + steerUUID + `","state":"started","session_id":"` + fake.nativeSession() + `"}`)
	late := writeBarrier(t, fake, stream, "conduit_test_barrier_late_lifecycle")
	if receipts := bodiesOf[provider.ControlReceiptEvent](late); len(receipts) != 0 {
		t.Fatalf("released steer was relabelled by late evidence: %#v", receipts)
	}
	snapshot := observe(t, s)
	if !snapshot.ActiveTurn.Present || snapshot.ActiveTurn.Value != started.Turn {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestStartTurnReleaseOfDifferentMessageIsStale(t *testing.T) {
	s, stream, _ := steerEndingTurn(t, nil, func(f *fakeClaude, input string) string { return queuedResultLine(f, "1", input) })
	receiveUntil(t, stream, isWarning("steer_delivery_unknown"))
	_, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.Some(provider.MessageID("other-message"))))
	requireStale(t, err, "mismatched release")
	// The steer is still unresolved: no release still refuses.
	_, err = s.StartTurn(context.Background(), nextTurnRequest(s, provider.None[provider.MessageID]()))
	requireStale(t, err, "start after mismatched release")
	if _, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.Some(provider.MessageID("steer-message")))); err != nil {
		t.Fatalf("matching release after mismatch: %v", err)
	}
}

func TestStartTurnReleaseWithNothingUnresolvedIsStale(t *testing.T) {
	fake := newFakeClaude(t)
	fake.onUser = func(f *fakeClaude, message map[string]any) { echoAndStart(f, message) }
	s, _ := openTestSession(t, fake, noneContract)
	_, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.Some(provider.MessageID("steer-message"))))
	requireStale(t, err, "release with nothing unresolved")
	if len(fake.messages("user")) != 0 {
		t.Fatal("rejected release dispatched a turn")
	}
	if _, err := s.StartTurn(context.Background(), nextTurnRequest(s, provider.None[provider.MessageID]())); err != nil {
		t.Fatalf("plain start after rejected release: %v", err)
	}
}
