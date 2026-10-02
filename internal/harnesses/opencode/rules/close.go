package rules

type CloseKind string

const (
	CloseAllowed                  CloseKind = "allowed"
	CloseBindingStale             CloseKind = "binding_stale"
	CloseActiveWorkRequiresDetach CloseKind = "active_work_requires_detach"
)

type CloseFacts struct {
	SessionMatches bool
	ActiveTurn     bool
	ExplicitDetach bool
}
type CloseDecision struct {
	Kind   CloseKind
	Reason string
}

// DecideClose permits closing the local handle without claiming shared-service quiescence.
func DecideClose(f CloseFacts) CloseDecision {
	if !f.SessionMatches {
		return CloseDecision{CloseBindingStale, StaleBindingReason}
	}
	if f.ActiveTurn && !f.ExplicitDetach {
		return CloseDecision{
			Kind:   CloseActiveWorkRequiresDetach,
			Reason: "active OpenCode work can only be detached because native stop is unproven",
		}
	}
	return CloseDecision{
		Kind:   CloseAllowed,
		Reason: "local session handle can be closed",
	}
}
