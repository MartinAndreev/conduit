package rules

// StartTurnKind describes this adapter's turn-start eligibility policy.
type StartTurnKind string

const (
	StartTurnAllowed StartTurnKind = "allowed"
	// StartTurnAllowedReleasingSteer allows the turn and clears the unresolved
	// steer that Conduit explicitly released; its delivery stays unknown.
	StartTurnAllowedReleasingSteer StartTurnKind = "allowed_releasing_steer"
	StartTurnBindingDiffers        StartTurnKind = "binding_differs"
	StartTurnUnavailable           StartTurnKind = "unavailable"
	StartTurnContractChanged       StartTurnKind = "contract_changed"
	// StartTurnReleaseNothingUnresolved rejects a release when no steer is
	// unresolved; the caller maps it to stale_binding.
	StartTurnReleaseNothingUnresolved StartTurnKind = "release_nothing_unresolved"
	// StartTurnReleaseMismatch rejects a release naming another message than
	// the unresolved steer; the caller maps it to stale_binding.
	StartTurnReleaseMismatch StartTurnKind = "release_mismatch"
)

type StartTurnFacts struct {
	SessionMatches            bool
	AssignmentIdentityMatches bool
	AssignmentRevisionMatches bool
	Closed                    bool
	ActiveTurn                bool
	SteerPending              bool
	// DeliveryUnknown is an earlier turn start whose delivery is unknown. It
	// is not a steer and cannot be released.
	DeliveryUnknown bool
	// SteerUnresolved is a steer whose session.prompt may have been admitted
	// and whose delivery native reconciliation has not decided.
	SteerUnresolved bool
	// ReleaseRequested is true when the request carries ReleaseUnresolvedSteer.
	ReleaseRequested bool
	// ReleaseMatchesUnresolvedSteer is true when the released MessageID is
	// exactly the unresolved steer's MessageID.
	ReleaseMatchesUnresolvedSteer bool
	SessionLost                   bool
	ResultContractMatches         bool
}

type StartTurnDecision struct {
	Kind   StartTurnKind
	Reason string
}

// DecideStartTurn owns OpenCode's revision and result-contract policy. A
// release is valid only for exactly the unresolved steer: a release when no
// steer is unresolved, or for another message, is rejected right after the
// binding check. A valid release lifts only the steer block; every other
// unavailability still refuses the turn, and then the steer stays unresolved.
// The caller keeps these facts, the release and the owned-process launch in
// the existing critical section.
func DecideStartTurn(facts StartTurnFacts) StartTurnDecision {
	if !facts.SessionMatches || !facts.AssignmentIdentityMatches || !facts.AssignmentRevisionMatches {
		return StartTurnDecision{
			Kind:   StartTurnBindingDiffers,
			Reason: "session or assignment binding differs",
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
	steerBlocks := facts.SteerUnresolved && !facts.ReleaseRequested
	if facts.Closed || facts.ActiveTurn || facts.SteerPending || facts.DeliveryUnknown || steerBlocks || facts.SessionLost {
		return StartTurnDecision{
			Kind:   StartTurnUnavailable,
			Reason: "session closed or a turn is already active",
		}
	}
	if !facts.ResultContractMatches {
		return StartTurnDecision{
			Kind:   StartTurnContractChanged,
			Reason: "result contract changed",
		}
	}
	if facts.ReleaseRequested {
		return StartTurnDecision{
			Kind:   StartTurnAllowedReleasingSteer,
			Reason: "Conduit released the unresolved steer; its delivery and the affected output attribution stay unknown",
		}
	}
	return StartTurnDecision{
		Kind:   StartTurnAllowed,
		Reason: "session is eligible to start an owned turn observer",
	}
}
