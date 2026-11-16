package rules

const StaleBindingReason = "session, turn, assignment, or revision binding is stale"

type SteerEligibilityKind string

const (
	SteerEligible     SteerEligibilityKind = "eligible"
	SteerBindingStale SteerEligibilityKind = "binding_stale"
	// SteerSessionClosed: the session handle is closed (Close may still be
	// stopping its client and server), so no correction is sent.
	SteerSessionClosed SteerEligibilityKind = "session_closed"
	// SteerSessionLost: the session is lost (its server or run observation
	// failed), so no correction can be delivered.
	SteerSessionLost SteerEligibilityKind = "session_lost"
)

type SteerEligibilityFacts struct {
	SessionMatches  bool
	ActiveTurn      bool
	TurnMatches     bool
	SteerPending    bool
	DeliveryUnknown bool
	SessionLost     bool
	Closed          bool
}
type SteerEligibilityDecision struct {
	Kind   SteerEligibilityKind
	Reason string
}

// DecideSteerEligibility checks the native lease before the business correction rule.
// A stale binding takes precedence over a closed session, which takes
// precedence over a lost one.
func DecideSteerEligibility(f SteerEligibilityFacts) SteerEligibilityDecision {
	if !f.SessionMatches {
		return SteerEligibilityDecision{SteerBindingStale, StaleBindingReason}
	}
	if f.Closed {
		return SteerEligibilityDecision{SteerSessionClosed, "the OpenCode session is closed; a correction cannot be sent"}
	}
	if f.SessionLost {
		return SteerEligibilityDecision{SteerSessionLost, "the OpenCode session is lost; a correction cannot be delivered"}
	}
	if !f.SessionMatches || !f.ActiveTurn || !f.TurnMatches || f.SteerPending || f.DeliveryUnknown {
		return SteerEligibilityDecision{SteerBindingStale, StaleBindingReason}
	}
	return SteerEligibilityDecision{
		Kind:   SteerEligible,
		Reason: "active session lease is eligible for correction",
	}
}

type SteerModeKind string

const (
	SteerModeAllowed     SteerModeKind = "allowed"
	SteerModeUnsupported SteerModeKind = "unsupported"
)

type SteerModeDecision struct {
	Kind   SteerModeKind
	Reason string
}

// DecideSteerMode is evaluated only after correction identity/revision validation.
func DecideSteerMode(activeTurnDelivery bool) SteerModeDecision {
	if !activeTurnDelivery {
		return SteerModeDecision{
			Kind:   SteerModeUnsupported,
			Reason: "only active-turn steer is supported",
		}
	}
	return SteerModeDecision{
		Kind:   SteerModeAllowed,
		Reason: "active-turn delivery is supported",
	}
}

type SteerReceiptKind string

const (
	SteerReceiptAccepted       SteerReceiptKind = "accepted"
	SteerReceiptBindingChanged SteerReceiptKind = "binding_changed"
)

type SteerReceiptFacts struct {
	ActiveTurn      bool
	TurnMatches     bool
	RevisionMatches bool
}
type SteerReceiptDecision struct {
	Kind   SteerReceiptKind
	Reason string
}

// DecideSteerReceipt checks the current lease after native transport acceptance.
func DecideSteerReceipt(f SteerReceiptFacts) SteerReceiptDecision {
	if !f.ActiveTurn || !f.TurnMatches || !f.RevisionMatches {
		return SteerReceiptDecision{
			Kind:   SteerReceiptBindingChanged,
			Reason: "binding changed after accepted delivery",
		}
	}
	return SteerReceiptDecision{
		Kind:   SteerReceiptAccepted,
		Reason: "accepted delivery still matches the active lease",
	}
}
