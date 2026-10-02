package rules

// StartTurnKind describes this adapter's turn-start eligibility policy.
type StartTurnKind string

const (
	StartTurnAllowed        StartTurnKind = "allowed"
	StartTurnBindingDiffers StartTurnKind = "binding_differs"
	StartTurnClosing        StartTurnKind = "closing"
	StartTurnOccupied       StartTurnKind = "occupied"
)

type StartTurnFacts struct {
	Closing                   bool
	TurnPending               bool
	ActiveTurn                bool
	SessionMatches            bool
	AssignmentIdentityMatches bool
}

type StartTurnDecision struct {
	Kind   StartTurnKind
	Reason string
}

// DecideStartTurn preserves Codex's identity-only binding policy: revision is
// deliberately not an eligibility fact. Facts and reservation stay under one lock.
func DecideStartTurn(facts StartTurnFacts) StartTurnDecision {
	if facts.Closing {
		return StartTurnDecision{
			Kind:   StartTurnClosing,
			Reason: "session is closing",
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
