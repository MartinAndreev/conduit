package rules

// StartTurnKind describes this adapter's turn-start eligibility policy.
type StartTurnKind string

const (
	StartTurnAllowed        StartTurnKind = "allowed"
	StartTurnBindingDiffers StartTurnKind = "binding_differs"
	StartTurnClosing        StartTurnKind = "closing"
	StartTurnOccupied       StartTurnKind = "occupied"
	// StartTurnReleaseNothingUnresolved rejects a ReleaseUnresolvedSteer when
	// no steer is unresolved; the caller maps it to stale_binding.
	StartTurnReleaseNothingUnresolved StartTurnKind = "release_nothing_unresolved"
	// StartTurnReleaseMismatch rejects a release naming another message than
	// the unresolved steer; the caller maps it to stale_binding.
	StartTurnReleaseMismatch StartTurnKind = "release_mismatch"
)

type StartTurnFacts struct {
	Closing                   bool
	TurnPending               bool
	ActiveTurn                bool
	SessionMatches            bool
	AssignmentIdentityMatches bool
	// SteerUnresolved is true while a dispatched turn/steer has no committed
	// reply. Codex never blocks a turn on it: the reply settles it, and a
	// stopped transport loses the session.
	SteerUnresolved bool
	// ReleaseRequested is true when the request carries ReleaseUnresolvedSteer.
	ReleaseRequested bool
	// ReleaseMatchesUnresolvedSteer is true when the released MessageID is
	// exactly the unresolved steer's MessageID.
	ReleaseMatchesUnresolvedSteer bool
}

type StartTurnDecision struct {
	Kind   StartTurnKind
	Reason string
}

// DecideStartTurn preserves Codex's identity-only binding policy: revision is
// deliberately not an eligibility fact. An explicit steer release follows the
// provider contract: it is rejected when no steer is unresolved or when it
// names another message. A matching release changes nothing here, because an
// unresolved steer never blocks a Codex turn and is still settled by its
// native reply. Facts and reservation stay under one lock.
func DecideStartTurn(facts StartTurnFacts) StartTurnDecision {
	if facts.Closing {
		return StartTurnDecision{
			Kind:   StartTurnClosing,
			Reason: "session is closing",
		}
	}
	if facts.ReleaseRequested && !facts.SteerUnresolved {
		return StartTurnDecision{
			Kind:   StartTurnReleaseNothingUnresolved,
			Reason: "steer release requested but no steer is unresolved",
		}
	}
	if facts.ReleaseRequested && !facts.ReleaseMatchesUnresolvedSteer {
		return StartTurnDecision{
			Kind:   StartTurnReleaseMismatch,
			Reason: "steer release names a different message than the unresolved steer",
		}
	}
	if facts.TurnPending || facts.ActiveTurn {
		return StartTurnDecision{
			Kind:   StartTurnOccupied,
			Reason: "session already has an active or unresolved turn",
		}
	}
	if !facts.SessionMatches || !facts.AssignmentIdentityMatches {
		return StartTurnDecision{
			Kind:   StartTurnBindingDiffers,
			Reason: "session or assignment binding differs",
		}
	}
	return StartTurnDecision{
		Kind:   StartTurnAllowed,
		Reason: "session is eligible to reserve a turn start",
	}
}
