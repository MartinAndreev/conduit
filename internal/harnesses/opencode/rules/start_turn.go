package rules

// StartTurnKind describes this adapter's turn-start eligibility policy.
type StartTurnKind string

const (
	StartTurnAllowed         StartTurnKind = "allowed"
	StartTurnBindingDiffers  StartTurnKind = "binding_differs"
	StartTurnUnavailable     StartTurnKind = "unavailable"
	StartTurnContractChanged StartTurnKind = "contract_changed"
)

type StartTurnFacts struct {
	SessionMatches            bool
	AssignmentIdentityMatches bool
	AssignmentRevisionMatches bool
	Closed                    bool
	ActiveTurn                bool
	SteerPending              bool
	DeliveryUnknown           bool
	SessionLost               bool
	ResultContractMatches     bool
}

type StartTurnDecision struct {
	Kind   StartTurnKind
	Reason string
}

// DecideStartTurn owns OpenCode's revision and result-contract policy. The caller
// keeps these facts and the owned-process launch in the existing critical section.
func DecideStartTurn(facts StartTurnFacts) StartTurnDecision {
	if !facts.SessionMatches || !facts.AssignmentIdentityMatches || !facts.AssignmentRevisionMatches {
		return StartTurnDecision{
			Kind:   StartTurnBindingDiffers,
			Reason: "session or assignment binding differs",
		}
	}
	if facts.Closed || facts.ActiveTurn || facts.SteerPending || facts.DeliveryUnknown || facts.SessionLost {
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
	return StartTurnDecision{
		Kind:   StartTurnAllowed,
		Reason: "session is eligible to start an owned turn observer",
	}
}
