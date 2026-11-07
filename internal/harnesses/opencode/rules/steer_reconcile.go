package rules

// SteerReconcileTriggerFacts describe whether native steer reconciliation may
// start now.
type SteerReconcileTriggerFacts struct {
	// SteerUnresolved is true while a possibly admitted steer is undecided.
	SteerUnresolved bool
	// Reconciling is true while a query for that steer is already running.
	Reconciling bool
	// ActiveTurn is true while the owned run observer of the steered turn is
	// still running; the steer may still be delivered into it.
	ActiveTurn bool
	// Unavailable is true once the session is closed or lost.
	Unavailable bool
}

type SteerReconcileTriggerKind string

const (
	SteerReconcileStart SteerReconcileTriggerKind = "start"
	SteerReconcileSkip  SteerReconcileTriggerKind = "skip"
)

type SteerReconcileTriggerDecision struct {
	Kind   SteerReconcileTriggerKind
	Reason string
}

// DecideSteerReconcileTrigger answers whether to query native delivery of an
// unresolved steer. It runs only after the turn ended, once per steer, and
// never for a closed or lost session.
func DecideSteerReconcileTrigger(f SteerReconcileTriggerFacts) SteerReconcileTriggerDecision {
	if !f.SteerUnresolved || f.Reconciling || f.Unavailable {
		return SteerReconcileTriggerDecision{
			Kind:   SteerReconcileSkip,
			Reason: "no unresolved steer, a query is already running, or the session is unavailable",
		}
	}
	if f.ActiveTurn {
		return SteerReconcileTriggerDecision{
			Kind:   SteerReconcileSkip,
			Reason: "the steered turn is still observed; the steer may still be delivered",
		}
	}
	return SteerReconcileTriggerDecision{
		Kind:   SteerReconcileStart,
		Reason: "the turn ended with an unresolved steer",
	}
}

// SteerReconcileFacts combine the native delivery query with the session
// state read again after the query.
type SteerReconcileFacts struct {
	// SameSteerUnresolved is false when the queried steer was released or
	// replaced while the query ran.
	SameSteerUnresolved bool
	// QueryFailed is true when the native query could not answer.
	QueryFailed bool
	// InHistory is true when the session message history holds the steer.
	InHistory bool
	// InInbox is true when the steer is admitted but not yet delivered.
	InInbox bool
	// RevisionMatches is true when the session assignment is still the
	// steer's expected revision.
	RevisionMatches bool
}

type SteerReconcileKind string

const (
	// SteerReconcileSuperseded: Conduit released the steer meanwhile; the
	// result is discarded.
	SteerReconcileSuperseded SteerReconcileKind = "superseded"
	// SteerReconcileDelivered: the steer is in the message history; the
	// caller advances the assignment like an accepted receipt.
	SteerReconcileDelivered SteerReconcileKind = "delivered"
	// SteerReconcileNotDelivered: the steer is neither in history nor in the
	// inbox; the caller releases the binding without advancing it.
	SteerReconcileNotDelivered SteerReconcileKind = "not_delivered"
	// SteerReconcileDoubtRemains: the steer stays unresolved until Conduit
	// releases it explicitly.
	SteerReconcileDoubtRemains SteerReconcileKind = "doubt_remains"
)

type SteerReconcileDecision struct {
	Kind   SteerReconcileKind
	Reason string
}

// DecideSteerReconcile answers what a native delivery query proves. History
// presence proves delivery; absence from both history and inbox proves
// non-delivery. A failed query, or a steer still waiting in the inbox (OpenCode
// may yet deliver it), keeps the doubt.
func DecideSteerReconcile(f SteerReconcileFacts) SteerReconcileDecision {
	if !f.SameSteerUnresolved {
		return SteerReconcileDecision{
			Kind:   SteerReconcileSuperseded,
			Reason: "the queried steer is no longer unresolved",
		}
	}
	if f.QueryFailed {
		return SteerReconcileDecision{
			Kind:   SteerReconcileDoubtRemains,
			Reason: "native steer delivery query failed",
		}
	}
	if f.InHistory {
		if !f.RevisionMatches {
			return SteerReconcileDecision{
				Kind:   SteerReconcileDoubtRemains,
				Reason: "steer was delivered but the assignment no longer has its expected revision",
			}
		}
		return SteerReconcileDecision{
			Kind:   SteerReconcileDelivered,
			Reason: "steer message is in the native session history",
		}
	}
	if f.InInbox {
		return SteerReconcileDecision{
			Kind:   SteerReconcileDoubtRemains,
			Reason: "steer is admitted to the native inbox and may still be delivered",
		}
	}
	return SteerReconcileDecision{
		Kind:   SteerReconcileNotDelivered,
		Reason: "steer message is in neither the native session history nor its inbox",
	}
}

// ReleasedSteerFacts combine a native query for one explicitly released
// steer, made after a turn ended, with the session state read again
// afterwards.
type ReleasedSteerFacts struct {
	// SameReleased is false when the queried release is no longer retained.
	SameReleased bool
	QueryFailed  bool
	// InHistory is true when the session message history holds the message.
	InHistory bool
	// IdleAfterMessage is true when the history holds an idle marker (an
	// ended execution) after the message.
	IdleAfterMessage bool
	// InInbox is true when the message is admitted but not yet delivered.
	InInbox bool
}

type ReleasedSteerKind string

const (
	// ReleasedSteerSuperseded: the queried release is no longer retained.
	ReleasedSteerSuperseded ReleasedSteerKind = "superseded"
	// ReleasedSteerFinished: the released steer can produce no more output.
	ReleasedSteerFinished ReleasedSteerKind = "finished"
	// ReleasedSteerMayRun: the released steer may still run; output stays
	// unattributed.
	ReleasedSteerMayRun ReleasedSteerKind = "may_run"
)

type ReleasedSteerDecision struct {
	Kind   ReleasedSteerKind
	Reason string
}

// DecideReleasedSteerSettled answers whether a released steer can still
// produce output. History presence alone never settles it: its admission time
// is unknown, so it may have been delivered into an execution that is still
// running. OpenCode appends an idle marker to the message history, which is
// ordered by a server-assigned append sequence (not by message ID), when
// an execution ends (succeeded, failed or interrupted); an idle marker after
// the message proves that the execution which received it has ended. This
// assumes an execution does not end while delivered input is left
// unprocessed. A message in neither history nor inbox (history, inbox,
// history re-read) was never admitted. A failed query, a message still in the
// inbox, or a delivered message without a later idle marker may still run.
func DecideReleasedSteerSettled(f ReleasedSteerFacts) ReleasedSteerDecision {
	if !f.SameReleased {
		return ReleasedSteerDecision{
			Kind:   ReleasedSteerSuperseded,
			Reason: "the queried released steer is no longer retained",
		}
	}
	if f.QueryFailed {
		return ReleasedSteerDecision{
			Kind:   ReleasedSteerMayRun,
			Reason: "native query for the released steer failed; output stays unattributed",
		}
	}
	if f.InHistory && f.IdleAfterMessage {
		return ReleasedSteerDecision{
			Kind:   ReleasedSteerFinished,
			Reason: "an execution ended after the released steer was delivered; it can produce no more output",
		}
	}
	if f.InHistory {
		return ReleasedSteerDecision{
			Kind:   ReleasedSteerMayRun,
			Reason: "released steer was delivered but no execution has ended since; output stays unattributed",
		}
	}
	if f.InInbox {
		return ReleasedSteerDecision{
			Kind:   ReleasedSteerMayRun,
			Reason: "released steer is still waiting in the native inbox; output stays unattributed",
		}
	}
	return ReleasedSteerDecision{
		Kind:   ReleasedSteerFinished,
		Reason: "released steer is in neither the native history nor the inbox; it was never admitted",
	}
}
