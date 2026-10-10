package rules

type CloseKind string

const (
	CloseAllowed                  CloseKind = "allowed"
	CloseBindingStale             CloseKind = "binding_stale"
	CloseActiveWorkRequiresDetach CloseKind = "active_work_requires_detach"
	CloseStopOwnedClient          CloseKind = "stop_owned_client"
)

type CloseFacts struct {
	SessionMatches     bool
	ActiveTurn         bool
	ExplicitDetach     bool
	InterruptThenClose bool
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
	if f.ActiveTurn && f.InterruptThenClose {
		return CloseDecision{
			Kind:   CloseStopOwnedClient,
			Reason: "owned OpenCode run client is terminated; native server execution remains unknown",
		}
	}
	if f.ActiveTurn && !f.ExplicitDetach {
		return CloseDecision{
			Kind:   CloseActiveWorkRequiresDetach,
			Reason: "active OpenCode work requires explicit_detach or interrupt_then_close",
		}
	}
	return CloseDecision{
		Kind:   CloseAllowed,
		Reason: "local session handle can be closed",
	}
}
