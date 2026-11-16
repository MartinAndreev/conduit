package execution

import (
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
)

func recordedEnvelope() Envelope {
	return Envelope{
		IntentID:       "intent-start",
		Kind:           IntentStartTurn,
		State:          StateRecorded,
		Version:        1,
		SessionID:      "session",
		Session:        startCommand().Session,
		Assignment:     assignment(3),
		TargetRevision: 3,
		RequestHash:    RequestHash(startCommand()),
		Memory:         MemoryWithManifest,
		ManifestFound:  true,
		Composition:    CompositionComposed,
		PayloadHash:    PayloadHash("memory"),
		ManifestHash:   PayloadHash("manifest"),
		PayloadFound:   true,
		PayloadStatus:  memory.PayloadPresent,
		PayloadText:    "memory",
		SessionFound:   true,
		CurrentSession: binding(),
	}
}

func TestDecideDispatchClaim(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Envelope)
		want   ClaimKind
	}{
		{"composed", func(*Envelope) {}, ClaimSend},
		{"no memory", func(e *Envelope) {
			e.Memory, e.ManifestFound, e.Composition, e.PayloadFound = MemoryNone, false, "", false
		}, ClaimSend},
		{"empty manifest without payload", func(e *Envelope) {
			e.Composition, e.PayloadHash, e.PayloadFound, e.PayloadStatus, e.PayloadText = CompositionEmpty, EmptyPayloadHash, false, "", ""
		}, ClaimSend},
		{"empty manifest with payload", func(e *Envelope) { e.Composition = CompositionEmpty }, ClaimInvalidFacts},
		{"composed without payload", func(e *Envelope) { e.PayloadFound = false }, ClaimInvalidFacts},
		{"removed payload while recorded", func(e *Envelope) {
			e.PayloadStatus, e.PayloadText = memory.PayloadRemovedBeforeDispatch, ""
		}, ClaimInvalidFacts},
		{"wrong hash", func(e *Envelope) { e.PayloadText = "other" }, ClaimInvalidFacts},
		{"manifest without memory", func(e *Envelope) { e.Memory = MemoryNone }, ClaimInvalidFacts},
		{"memory without manifest", func(e *Envelope) { e.ManifestFound = false }, ClaimInvalidFacts},
		{"unknown composition", func(e *Envelope) { e.Composition = "" }, ClaimInvalidFacts},
		{"unknown state", func(e *Envelope) { e.State = "sent" }, ClaimInvalidFacts},
		{"recorded with outcome", func(e *Envelope) { e.OutcomeFound = true }, ClaimInvalidFacts},
		{"session gone", func(e *Envelope) { e.SessionFound = false }, ClaimBindingChanged},
		{"revision moved", func(e *Envelope) { e.CurrentSession.Assignment.Revision = 4 }, ClaimBindingChanged},
		{"assignment changed", func(e *Envelope) { e.CurrentSession.Assignment.RunID = "other" }, ClaimBindingChanged},
		{"adapter session changed", func(e *Envelope) { e.CurrentSession.AdapterSessionID = "other" }, ClaimBindingChanged},
		{"dispatching", func(e *Envelope) { e.State = StateDispatching }, ClaimDeliveryUnknown},
		{"dispatching with outcome", func(e *Envelope) { e.State, e.OutcomeFound = StateDispatching, true }, ClaimInvalidFacts},
		{"invalidated", func(e *Envelope) {
			e.State, e.PayloadStatus, e.PayloadText = StateInvalidated, memory.PayloadRemovedBeforeDispatch, ""
		}, ClaimInvalidated},
		{"invalidated with outcome", func(e *Envelope) { e.State, e.OutcomeFound = StateInvalidated, true }, ClaimInvalidFacts},
		{"unknown without outcome", func(e *Envelope) { e.State = StateUnknown }, ClaimInvalidFacts},
		{"diverged without outcome", func(e *Envelope) { e.State = StateDiverged }, ClaimInvalidFacts},
		{"accepted without outcome", func(e *Envelope) { e.State = StateAccepted }, ClaimInvalidFacts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := recordedEnvelope()
			tc.change(&e)
			if got := DecideDispatchClaim(ClaimFacts{Found: true, Envelope: e}); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
	if got := DecideDispatchClaim(ClaimFacts{}); got.Kind != ClaimInvalidFacts {
		t.Fatalf("missing intent = %+v", got)
	}
}

// A settled intent returns its outcome even when its payload was removed
// since or its session moved on.
func TestDecideDispatchClaimRecordedOutcome(t *testing.T) {
	for _, state := range []IntentState{StateAccepted, StateRejected, StateUnknown, StateDiverged} {
		e := recordedEnvelope()
		e.State, e.OutcomeFound = state, true
		e.PayloadStatus, e.PayloadText = memory.PayloadRemovedAfterDelivery, ""
		e.CurrentSession.Assignment.Revision = 9
		if got := DecideDispatchClaim(ClaimFacts{Found: true, Envelope: e}); got.Kind != ClaimRecordedOutcome {
			t.Fatalf("%s = %+v", state, got)
		}
	}
}
