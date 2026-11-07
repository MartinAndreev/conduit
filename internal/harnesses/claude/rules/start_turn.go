package rules

type StartTurnKind string

const (
	StartTurnAllowed         StartTurnKind = "allowed"
	StartTurnClosing         StartTurnKind = "closing"
	StartTurnOccupied        StartTurnKind = "occupied"
	StartTurnBindingDiffers  StartTurnKind = "binding_differs"
	StartTurnContractChanged StartTurnKind = "contract_changed"
)

type StartTurnFacts struct {
	// Closing is true once Close reserved shutdown or the session was lost.
	Closing bool
	// ActiveTurn is true while a dispatched turn has not observed its result.
	ActiveTurn bool
	// SteerUnresolved is true while a dispatched steer has neither delivery
	// evidence nor a known outcome, so the current revision is unresolved.
	SteerUnresolved           bool
	SessionMatches            bool
	AssignmentIdentityMatches bool
	// ResultContractMatches is true when the request names the contract the
	// process was launched with; --json-schema cannot change per turn.
	ResultContractMatches bool
}

type StartTurnDecision struct {
	Kind   StartTurnKind
	Reason string
}

// DecideStartTurn answers whether a new turn may be reserved. The caller keeps
// these facts and the reservation in one critical section.
func DecideStartTurn(f StartTurnFacts) StartTurnDecision {
	if f.Closing {
		return StartTurnDecision{
			Kind:   StartTurnClosing,
			Reason: "session is closing or lost",
		}
	}
	if f.ActiveTurn || f.SteerUnresolved {
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
	return StartTurnDecision{
		Kind:   StartTurnAllowed,
		Reason: "session is eligible to dispatch a turn",
	}
}
