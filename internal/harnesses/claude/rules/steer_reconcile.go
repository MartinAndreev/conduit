package rules

// UnobservedSteerFacts describe an unresolved steer when the turn it was
// dispatched into ends with a terminal native result. Any delivery evidence
// (replay echo, command_lifecycle started, or a result naming the steer UUID)
// would already have resolved the steer, so SteerUnresolved implies none was
// observed.
type UnobservedSteerFacts struct {
	// SteerUnresolved is true while a dispatched steer lacks delivery evidence.
	SteerUnresolved bool
	// Dispatch is how far the steer's write had progressed when the terminal
	// result was read.
	Dispatch SteerDispatch
	// LifecycleObserved is true when any command_lifecycle (queued, cancelled,
	// ...) named the steer UUID: claude saw the message, so its fate is open.
	LifecycleObserved bool
	// QueuedTurnCountKnown is false when the terminal result has no
	// queued_turn_count field.
	QueuedTurnCountKnown bool
	QueuedTurnCount      uint64
}

// SteerDispatch is the local write progress of an unresolved steer, as the
// reader saw it when it read the terminal result.
type SteerDispatch string

const (
	// SteerReserved: reserved, not yet handed to the writer. It can still be
	// withdrawn so that it is never written.
	SteerReserved SteerDispatch = "reserved"
	// SteerDispatching: handed to the writer; whether the write completed
	// before the result was read is unknown.
	SteerDispatching SteerDispatch = "dispatching"
	// SteerWritten: the write completed before the result was read.
	SteerWritten SteerDispatch = "written"
)

type UnobservedSteerKind string

const (
	// UnobservedSteerNone: no steer is unresolved; nothing to reconcile.
	UnobservedSteerNone UnobservedSteerKind = "none"
	// UnobservedSteerNotObserved: claude ended the turn without observing the
	// steer and has nothing queued, so the steer was not delivered. The
	// caller releases the binding without advancing the assignment.
	UnobservedSteerNotObserved UnobservedSteerKind = "not_observed"
	// UnobservedSteerWithdrawn: the steer was never handed to the writer;
	// the caller withdraws it, so it is never sent and Steer fails not-sent.
	UnobservedSteerWithdrawn UnobservedSteerKind = "withdrawn"
	// UnobservedSteerDoubtRemains: late evidence may still arrive; the steer
	// stays unresolved until it does or Conduit releases it explicitly.
	UnobservedSteerDoubtRemains UnobservedSteerKind = "doubt_remains"
)

type UnobservedSteerDecision struct {
	Kind   UnobservedSteerKind
	Reason string
}

// DecideUnobservedSteer answers, at a terminal result, whether an unresolved
// steer is proven not observed by claude. Only a steer whose write completed
// before the result was read can be judged; a steer still only reserved is
// withdrawn, and one whose write was in flight keeps the doubt. Only the conjunction of no lifecycle
// for the steer UUID and a known queued_turn_count of zero proves it; an
// absent count or queued work keeps the doubt.
func DecideUnobservedSteer(f UnobservedSteerFacts) UnobservedSteerDecision {
	if !f.SteerUnresolved {
		return UnobservedSteerDecision{
			Kind:   UnobservedSteerNone,
			Reason: "no steer is unresolved",
		}
	}
	if f.Dispatch == SteerReserved {
		return UnobservedSteerDecision{
			Kind:   UnobservedSteerWithdrawn,
			Reason: "turn ended before the steer was handed to the writer; it is withdrawn and never sent",
		}
	}
	if f.Dispatch != SteerWritten {
		return UnobservedSteerDecision{
			Kind:   UnobservedSteerDoubtRemains,
			Reason: "the steer write may have completed only after the terminal result was read",
		}
	}
	if f.LifecycleObserved {
		return UnobservedSteerDecision{
			Kind:   UnobservedSteerDoubtRemains,
			Reason: "claude reported a command lifecycle for the steer without delivery evidence",
		}
	}
	if !f.QueuedTurnCountKnown {
		return UnobservedSteerDecision{
			Kind:   UnobservedSteerDoubtRemains,
			Reason: "terminal result does not report queued_turn_count",
		}
	}
	if f.QueuedTurnCount > 0 {
		return UnobservedSteerDecision{
			Kind:   UnobservedSteerDoubtRemains,
			Reason: "claude still has queued turns that may contain the steer",
		}
	}
	return UnobservedSteerDecision{
		Kind:   UnobservedSteerNotObserved,
		Reason: "turn ended with no echo, no command lifecycle for the steer and no queued turns; claude did not observe the steer",
	}
}
