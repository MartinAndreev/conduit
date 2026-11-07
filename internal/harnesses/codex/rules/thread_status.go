package rules

// ThreadStatusFacts describe the local session lifecycle when a native
// thread/status/changed observation (idle or active) arrives.
type ThreadStatusFacts struct {
	// Closing is true once Close reserved shutdown of the owned App Server.
	Closing bool
	// Ended is true when the local session is already lost or closed.
	Ended bool
}

type ThreadStatusKind string

const (
	ThreadStatusApply             ThreadStatusKind = "apply"
	ThreadStatusIgnoredWhileEnded ThreadStatusKind = "ignored_while_ended"
)

type ThreadStatusDecision struct {
	Kind   ThreadStatusKind
	Reason string
}

// DecideThreadStatusTransition answers whether a native idle/active thread
// status may replace the local session state. A late native status cannot
// reopen a session that is closing, closed or lost: those transitions are owned
// by Close and transport/process loss, not by App Server status reports.
func DecideThreadStatusTransition(f ThreadStatusFacts) ThreadStatusDecision {
	if f.Closing || f.Ended {
		return ThreadStatusDecision{
			Kind:   ThreadStatusIgnoredWhileEnded,
			Reason: "native thread status cannot overwrite a closing, closed or lost session",
		}
	}
	return ThreadStatusDecision{
		Kind:   ThreadStatusApply,
		Reason: "native thread status updates an open session",
	}
}
