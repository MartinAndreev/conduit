package rules

type CloseFacts struct {
	SessionMatches         bool
	Closing                bool
	RequireQuiescent       bool
	ActiveOrUnresolvedWork bool
	ExplicitDetach         bool
}
type CloseKind string

const (
	CloseAllowed           CloseKind = "allowed"
	CloseSessionDiffers    CloseKind = "session_differs"
	CloseAlreadyClosing    CloseKind = "already_closing"
	CloseWorkUnresolved    CloseKind = "work_unresolved"
	CloseDetachUnsupported CloseKind = "detach_unsupported"
)

type CloseDecision struct {
	Kind   CloseKind
	Reason string
}

func DecideClose(f CloseFacts) CloseDecision {
	if !f.SessionMatches {
		return CloseDecision{
			Kind:   CloseSessionDiffers,
			Reason: "session reference differs",
		}
	}
	if f.Closing {
		return CloseDecision{
			Kind:   CloseAlreadyClosing,
			Reason: "session is already closing",
		}
	}
	if f.RequireQuiescent && f.ActiveOrUnresolvedWork {
		return CloseDecision{
			Kind:   CloseWorkUnresolved,
			Reason: "session has active or unresolved work",
		}
	}
	if f.ExplicitDetach {
		return CloseDecision{
			Kind:   CloseDetachUnsupported,
			Reason: "owned App Server cannot be detached",
		}
	}
	return CloseDecision{
		Kind:   CloseAllowed,
		Reason: "owned App Server is eligible to close",
	}
}

type OwnedProcessTerminationKind string

const (
	OwnedProcessKillRequired      OwnedProcessTerminationKind = "kill_required"
	OwnedProcessRootAlreadyReaped OwnedProcessTerminationKind = "root_already_reaped"
)

type OwnedProcessTerminationDecision struct {
	Kind   OwnedProcessTerminationKind
	Reason string
}

func DecideOwnedProcessTermination(rootReaped, treeKillSupported bool) OwnedProcessTerminationDecision {
	if treeKillSupported || !rootReaped {
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

// Only owned-process evidence proves resources released; a reaped Windows root
// alone cannot establish descendant quiescence.
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
