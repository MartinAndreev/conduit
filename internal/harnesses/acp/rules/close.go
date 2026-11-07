package rules

// CloseKind describes whether and how the owned agent may be closed.
type CloseKind string

const (
	CloseAllowed            CloseKind = "allowed"
	CloseCancelThenClose    CloseKind = "cancel_then_close"
	CloseBindingStale       CloseKind = "binding_stale"
	CloseAlreadyClosing     CloseKind = "already_closing"
	CloseWorkUnresolved     CloseKind = "work_unresolved"
	CloseDetachUnsupported  CloseKind = "detach_unsupported"
	CloseUnresolvedNoPolicy CloseKind = "unresolved_no_policy"
)

type CloseFacts struct {
	SessionMatches     bool
	Closing            bool
	ActiveWork         bool
	RequireQuiescent   bool
	ExplicitDetach     bool
	InterruptThenClose bool
}

type CloseDecision struct {
	Kind   CloseKind
	Reason string
}

// DecideClose follows the owned-process close policy: an idle session closes
// under any policy. With an unresolved prompt, interrupt_then_close cancels
// first; explicit_detach cannot be honoured because the agent process is owned
// and does not outlive its stdin; require_quiescent is refused.
func DecideClose(f CloseFacts) CloseDecision {
	if !f.SessionMatches {
		return CloseDecision{
			Kind:   CloseBindingStale,
			Reason: "session reference differs",
		}
	}
	if f.Closing {
		return CloseDecision{
			Kind:   CloseAlreadyClosing,
			Reason: "session is already closing",
		}
	}
	if !f.ActiveWork {
		return CloseDecision{
			Kind:   CloseAllowed,
			Reason: "no unresolved prompt; owned agent is eligible to close",
		}
	}
	switch {
	case f.InterruptThenClose:
		return CloseDecision{
			Kind:   CloseCancelThenClose,
			Reason: "unresolved prompt is cancelled before the owned agent closes",
		}
	case f.ExplicitDetach:
		return CloseDecision{
			Kind:   CloseDetachUnsupported,
			Reason: "owned ACP agent cannot be detached from its stdio",
		}
	case f.RequireQuiescent:
		return CloseDecision{
			Kind:   CloseWorkUnresolved,
			Reason: "session has an unresolved prompt",
		}
	default:
		return CloseDecision{
			Kind:   CloseUnresolvedNoPolicy,
			Reason: "unresolved prompt requires interrupt_then_close",
		}
	}
}

type OwnedProcessTerminationKind string

const (
	OwnedProcessKillRequired      OwnedProcessTerminationKind = "kill_required"
	OwnedProcessRootAlreadyReaped OwnedProcessTerminationKind = "root_already_reaped"
)

type OwnedProcessTerminationFacts struct {
	RootReaped        bool
	TreeKillSupported bool
}

type OwnedProcessTerminationDecision struct {
	Kind   OwnedProcessTerminationKind
	Reason string
}

// DecideOwnedProcessTermination always signals the owned group where tree kill
// is supported: a reaped root does not prove its descendants are gone.
func DecideOwnedProcessTermination(f OwnedProcessTerminationFacts) OwnedProcessTerminationDecision {
	if f.TreeKillSupported || !f.RootReaped {
		return OwnedProcessTerminationDecision{
			Kind:   OwnedProcessKillRequired,
			Reason: "owned process termination must be attempted",
		}
	}
	return OwnedProcessTerminationDecision{
		Kind:   OwnedProcessRootAlreadyReaped,
		Reason: "root was reaped but descendant termination is unsupported",
	}
}

type QuiescenceFacts struct {
	RootReaped        bool
	TreeKillSupported bool
	KillAttempted     bool
	KillConfirmed     bool
}

type QuiescenceKind string

const (
	QuiescenceConfirmed QuiescenceKind = "confirmed"
	QuiescenceUnproven  QuiescenceKind = "unproven"
)

type QuiescenceDecision struct {
	Kind   QuiescenceKind
	Reason string
}

// DecideQuiescence reports released resources only with owned-group evidence.
func DecideQuiescence(f QuiescenceFacts) QuiescenceDecision {
	if f.RootReaped && f.TreeKillSupported && f.KillAttempted && f.KillConfirmed {
		return QuiescenceDecision{
			Kind:   QuiescenceConfirmed,
			Reason: "owned process group termination and root reaping are confirmed",
		}
	}
	return QuiescenceDecision{
		Kind:   QuiescenceUnproven,
		Reason: "owned process group quiescence remains unknown",
	}
}
