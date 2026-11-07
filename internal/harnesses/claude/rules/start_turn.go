package rules

type StartTurnKind string

const (
	StartTurnAllowed StartTurnKind = "allowed"
	// StartTurnAllowedReleasingSteer allows the turn and clears the unresolved
	// steer that Conduit explicitly released; its delivery stays unknown.
	StartTurnAllowedReleasingSteer StartTurnKind = "allowed_releasing_steer"
	StartTurnClosing               StartTurnKind = "closing"
	StartTurnOccupied              StartTurnKind = "occupied"
	StartTurnBindingDiffers        StartTurnKind = "binding_differs"
	StartTurnContractChanged       StartTurnKind = "contract_changed"
	// StartTurnReleaseNothingUnresolved rejects a release when no steer is
	// unresolved; the caller maps it to stale_binding.
	StartTurnReleaseNothingUnresolved StartTurnKind = "release_nothing_unresolved"
	// StartTurnReleaseMismatch rejects a release naming another message than
	// the unresolved steer; the caller maps it to stale_binding.
	StartTurnReleaseMismatch StartTurnKind = "release_mismatch"
)

type StartTurnFacts struct {
	// Closing is true once Close reserved shutdown or the session was lost.
	Closing bool
	// ActiveTurn is true while a dispatched turn has not observed its result.
	ActiveTurn bool
	// SteerUnresolved is true while a dispatched steer has neither delivery
	// evidence nor a known outcome, so the current revision is unresolved.
	SteerUnresolved bool
	// ReleaseRequested is true when the request carries
	// ReleaseUnresolvedSteer.
	ReleaseRequested bool
	// ReleaseMatchesUnresolvedSteer is true when the released MessageID is
	// exactly the unresolved steer's MessageID.
	ReleaseMatchesUnresolvedSteer bool
	SessionMatches                bool
	AssignmentIdentityMatches     bool
	// ResultContractMatches is true when the request names the contract the
	// process was launched with; --json-schema cannot change per turn.
	ResultContractMatches bool
}

type StartTurnDecision struct {
	Kind   StartTurnKind
	Reason string
}

// DecideStartTurn answers whether a new turn may be reserved. An explicit
// release is valid only for exactly the unresolved steer; a release when no
// steer is unresolved, or for another message, is rejected before any other
// eligibility fact except closing. A valid release lifts only the steer block:
// an active turn, a differing binding or a changed contract still refuse the
// turn, and then the steer stays unresolved. The caller keeps these facts, the
// reservation and the release in one critical section.
func DecideStartTurn(f StartTurnFacts) StartTurnDecision {
	if f.Closing {
		return StartTurnDecision{
			Kind:   StartTurnClosing,
			Reason: "session is closing or lost",
		}
	}
	if f.ReleaseRequested && !f.SteerUnresolved {
		return StartTurnDecision{
			Kind:   StartTurnReleaseNothingUnresolved,
			Reason: "steer release requested but no steer is unresolved",
		}
	}
	if f.ReleaseRequested && !f.ReleaseMatchesUnresolvedSteer {
		return StartTurnDecision{
			Kind:   StartTurnReleaseMismatch,
			Reason: "steer release names a different message than the unresolved steer",
		}
	}
	steerBlocks := f.SteerUnresolved && !f.ReleaseRequested
	if f.ActiveTurn || steerBlocks {
		return StartTurnDecision{
			Kind:   StartTurnOccupied,
			Reason: "session has an active turn or an unresolved correction",
		}
	}
	if !f.SessionMatches || !f.AssignmentIdentityMatches {
		return StartTurnDecision{
			Kind:   StartTurnBindingDiffers,
			Reason: "session or assignment binding differs",
		}
	}
	if !f.ResultContractMatches {
		return StartTurnDecision{
			Kind:   StartTurnContractChanged,
			Reason: "result contract differs from the one negotiated at launch",
		}
	}
	if f.ReleaseRequested {
		return StartTurnDecision{
			Kind:   StartTurnAllowedReleasingSteer,
			Reason: "Conduit released the unresolved steer; its delivery and the affected output attribution stay unknown",
		}
	}
	return StartTurnDecision{
		Kind:   StartTurnAllowed,
		Reason: "session is eligible to dispatch a turn",
	}
}
