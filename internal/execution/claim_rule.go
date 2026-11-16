package execution

import (
	"slices"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Envelope is a recorded intent as the claim transaction reads it: the
// intent, its manifest and payload, its outcome if one was recorded, and the
// session's current binding. A send is built only from it and the command
// whose request hash it carries.
type Envelope struct {
	IntentID  IntentID
	Kind      IntentKind
	State     IntentState
	Version   int64
	SessionID sessions.ID
	Session   provider.SessionRef
	// Assignment is the intent's assignment at its expected revision.
	Assignment     provider.AssignmentRef
	TargetRevision uint64
	ExpectedTurn   provider.TurnRef
	Mode           provider.DeliveryMode
	RequestHash    string
	Memory         MemoryPresence

	ManifestFound bool
	Composition   ManifestComposition
	PayloadHash   string
	ManifestHash  string

	PayloadFound  bool
	PayloadStatus memory.PayloadStatus
	// PayloadText is the stored text; empty unless the status is present.
	PayloadText string

	OutcomeFound bool
	Outcome      OutcomeRecord

	SessionFound   bool
	CurrentSession SessionBinding
}

// ClaimFacts are the envelope read in the claim transaction.
type ClaimFacts struct {
	Found    bool
	Envelope Envelope
}

type ClaimKind string

const (
	// ClaimSend: the intent is claimed and may be sent.
	ClaimSend ClaimKind = "claim"
	// ClaimRecordedOutcome: an outcome is recorded; nothing is sent and the
	// procedure returns it as on the first call.
	ClaimRecordedOutcome ClaimKind = "recorded_outcome"
	// ClaimDeliveryUnknown: a claim without an outcome; the send may have
	// happened, so it is never repeated.
	ClaimDeliveryUnknown ClaimKind = "delivery_unknown"
	ClaimInvalidated     ClaimKind = "invalidated"
	ClaimBindingChanged  ClaimKind = "binding_changed"
	ClaimInvalidFacts    ClaimKind = "invalid_facts"
)

type ClaimDecision struct {
	Kind   ClaimKind
	Reason string
}

func claimDecision(kind ClaimKind, reason string) ClaimDecision {
	return ClaimDecision{Kind: kind, Reason: reason}
}

// DecideDispatchClaim is the retry eligibility of a recorded intent. It
// answers two separate questions: whether a send is allowed and whether an
// outcome is recorded.
//
// An outcome row must exist exactly for the settled and open-after-send
// states (accepted, rejected, unknown, diverged); its absence there, or its
// presence on recorded, dispatching or invalidated, is invalid. Those states
// return the recorded outcome, dispatching is delivery_unknown and
// invalidated is invalidated, whatever the payload's status by now. Only a
// recorded intent may be claimed: its manifest must match its memory, a
// composed manifest needs a present payload whose text hashes to the stored
// payload hash, an empty one has no payload row, and the session must still
// have the intent's binding and revision.
func DecideDispatchClaim(f ClaimFacts) ClaimDecision {
	if !f.Found {
		return claimDecision(ClaimInvalidFacts, "intent is not stored")
	}
	e := f.Envelope
	if !slices.Contains(IntentStates(), e.State) {
		return claimDecision(ClaimInvalidFacts, "intent state is unknown")
	}
	settled := slices.Contains([]IntentState{StateAccepted, StateRejected, StateUnknown, StateDiverged}, e.State)
	if settled != e.OutcomeFound {
		return claimDecision(ClaimInvalidFacts, "an outcome row exists exactly for accepted, rejected, unknown and diverged intents")
	}
	switch e.State {
	case StateAccepted, StateRejected, StateUnknown, StateDiverged:
		return claimDecision(ClaimRecordedOutcome, "an outcome is recorded; nothing is sent again")
	case StateDispatching:
		return claimDecision(ClaimDeliveryUnknown, "the intent was claimed without a recorded outcome; the send may have happened")
	case StateInvalidated:
		return claimDecision(ClaimInvalidated, "the intent was invalidated before sending")
	}
	if reason := envelopeProblem(e); reason != "" {
		return claimDecision(ClaimInvalidFacts, reason)
	}
	if !e.SessionFound || !e.CurrentSession.Assignment.SameIdentity(e.Assignment) ||
		e.CurrentSession.Assignment.Revision != e.Assignment.Revision || e.CurrentSession.AdapterSessionID != e.Session.AdapterSessionID {
		return claimDecision(ClaimBindingChanged, "the session's binding or revision changed since the intent was recorded")
	}
	return claimDecision(ClaimSend, "recorded intent may be sent")
}

// envelopeProblem checks a recorded intent's manifest and payload.
func envelopeProblem(e Envelope) string {
	switch {
	case e.Memory == MemoryNone && e.ManifestFound:
		return "intent without memory has a manifest"
	case e.Memory == MemoryWithManifest && !e.ManifestFound:
		return "intent with memory has no manifest"
	case e.Memory != MemoryNone && e.Memory != MemoryWithManifest:
		return "intent memory presence is unknown"
	case !e.ManifestFound:
		return ""
	}
	switch e.Composition {
	case CompositionEmpty:
		if e.PayloadFound {
			return "empty manifest has a payload row"
		}
		return ""
	case CompositionComposed:
	default:
		return "manifest composition is unknown"
	}
	switch {
	case !e.PayloadFound:
		return "composed manifest has no payload row"
	case e.PayloadStatus != memory.PayloadPresent:
		return "recorded intent's payload is not present"
	case PayloadHash(e.PayloadText) != e.PayloadHash:
		return "payload text does not hash to the manifest's payload hash"
	}
	return ""
}
