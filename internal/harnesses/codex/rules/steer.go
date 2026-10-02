package rules

// SteerFacts contains only the adapter facts; assignment transitions remain
// owned by sessions.DecideCorrection after technical eligibility is established.
type SteerFacts struct {
	SessionMatches    bool
	ActiveTurnMode    bool
	ActiveTurnMatches bool
	SteerPending      bool
	TurnPending       bool
}
type SteerKind string

const (
	SteerAllowed           SteerKind = "allowed"
	SteerSessionDiffers    SteerKind = "session_differs"
	SteerModeUnsupported   SteerKind = "mode_unsupported"
	SteerBindingUnresolved SteerKind = "binding_unresolved"
)

type SteerDecision struct {
	Kind   SteerKind
	Reason string
}

func DecideSteer(f SteerFacts) SteerDecision {
	if !f.SessionMatches {
		return SteerDecision{
			Kind:   SteerSessionDiffers,
			Reason: "session reference differs",
		}
	}
	if !f.ActiveTurnMode {
		return SteerDecision{
			Kind:   SteerModeUnsupported,
			Reason: "only active-turn steering is supported",
		}
	}
	if !f.ActiveTurnMatches || f.SteerPending || f.TurnPending {
		return SteerDecision{
			Kind:   SteerBindingUnresolved,
			Reason: "active turn is absent, different, or unresolved",
		}
	}
	return SteerDecision{
		Kind:   SteerAllowed,
		Reason: "active turn is eligible for correction validation",
	}
}

type SteerReceiptFacts struct {
	PendingRequestMatches bool
	ActiveTurnMatches     bool
	NativeTurnMatches     bool
}
type SteerReceiptKind string

const (
	SteerReceiptAccepted       SteerReceiptKind = "accepted"
	SteerReceiptBindingDiffers SteerReceiptKind = "binding_differs"
)

type SteerReceiptDecision struct {
	Kind   SteerReceiptKind
	Reason string
}

// Decode failures stay in the procedure; this rule assesses decoded correlation.
func DecideSteerReceipt(f SteerReceiptFacts) SteerReceiptDecision {
	if !f.PendingRequestMatches || !f.ActiveTurnMatches || !f.NativeTurnMatches {
		return SteerReceiptDecision{
			Kind:   SteerReceiptBindingDiffers,
			Reason: "turn/steer response does not match the pending active turn",
		}
	}
	return SteerReceiptDecision{
		Kind:   SteerReceiptAccepted,
		Reason: "turn/steer response matches the pending active turn",
	}
}
