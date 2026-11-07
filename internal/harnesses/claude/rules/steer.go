package rules

// SteerFacts contain only adapter facts; the assignment transition remains
// owned by sessions.DecideCorrection after technical eligibility is established.
type SteerFacts struct {
	SessionMatches    bool
	ActiveTurnMode    bool
	ActiveTurnMatches bool
	// TurnEnding is true once the active turn's terminal result was observed
	// and the turn is about to be cleared.
	TurnEnding bool
	// SteerUnresolved is true while an earlier steer lacks delivery evidence.
	SteerUnresolved bool
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

// DecideSteer answers whether a correction may be dispatched to the active
// turn. One unresolved correction at a time keeps attribution reconcilable.
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
	if !f.ActiveTurnMatches || f.TurnEnding || f.SteerUnresolved {
		return SteerDecision{
			Kind:   SteerBindingUnresolved,
			Reason: "active turn is absent, different, ending, or has an unresolved correction",
		}
	}
	return SteerDecision{
		Kind:   SteerAllowed,
		Reason: "active turn is eligible for correction validation",
	}
}
