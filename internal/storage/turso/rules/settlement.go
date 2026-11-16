package rules

import (
	"fmt"
	"slices"
	"time"

	"github.com/MartinAndreev/conduit/internal/execution"
)

// SettlementCommit is how far one settlement transaction got with its
// COMMIT.
type SettlementCommit string

const (
	// SettlementCommitNotReached: the attempt failed before COMMIT and was
	// rolled back (or its cleanup failed).
	SettlementCommitNotReached SettlementCommit = "not_reached"
	SettlementCommitted        SettlementCommit = "committed"
	// SettlementCommitFailedRolledBack: COMMIT failed and ROLLBACK succeeded,
	// so the attempt changed nothing.
	SettlementCommitFailedRolledBack SettlementCommit = "failed_rolled_back"
	// SettlementCommitFailedUnknown: COMMIT failed and ROLLBACK failed too;
	// whether the attempt took effect is unknown.
	SettlementCommitFailedUnknown SettlementCommit = "failed_unknown"
)

func settlementCommits() []SettlementCommit {
	return []SettlementCommit{SettlementCommitNotReached, SettlementCommitted, SettlementCommitFailedRolledBack, SettlementCommitFailedUnknown}
}

// SettlementRetryFacts describe one failed settlement attempt, each fact
// read separately rather than from a joined error.
type SettlementRetryFacts struct {
	// Busy reports that the attempt itself failed because another
	// connection held the write lock; a cleanup failure is not part of it.
	Busy bool
	// CleanupFailed reports that ending the attempt's transaction or
	// releasing its connection failed.
	CleanupFailed bool
	Commit        SettlementCommit
	// DeadlineExceeded reports that the settlement's attempt budget ended.
	DeadlineExceeded bool
	// Attempts counts the attempts made so far, including the failed one.
	Attempts int
}

type SettlementRetryKind string

const (
	SettlementRetry SettlementRetryKind = "retry"
	// SettlementNotRetryable returns the attempt's failure.
	SettlementNotRetryable SettlementRetryKind = "not_retryable"
	SettlementDeadline     SettlementRetryKind = "deadline"
	SettlementExhausted    SettlementRetryKind = "exhausted"
)

type SettlementRetryDecision struct {
	Kind   SettlementRetryKind
	Reason string
}

// SettlementMaxAttempts bounds the settlement transactions, the first
// included; each already waits up to the connection busy timeout.
const SettlementMaxAttempts = 3

// DecideSettlementRetry answers whether a failed settlement transaction runs
// again. The harness is never called again; only the database transaction
// repeats, and only after a busy failure that provably changed nothing. A
// failed cleanup or a COMMIT whose rollback also failed leaves the effect on
// the database unknown and is never retried; a non-busy failure is returned;
// then the budget and the bound decide.
func DecideSettlementRetry(f SettlementRetryFacts) SettlementRetryDecision {
	switch {
	case f.Attempts < 1 || !slices.Contains(settlementCommits(), f.Commit) || f.Commit == SettlementCommitted:
		return SettlementRetryDecision{Kind: SettlementNotRetryable, Reason: fmt.Sprintf("invalid settlement attempt facts (attempts %d, commit %q)", f.Attempts, f.Commit)}
	case f.CleanupFailed || f.Commit == SettlementCommitFailedUnknown:
		return SettlementRetryDecision{Kind: SettlementNotRetryable, Reason: "settlement cleanup failed; its effect on the database is unknown"}
	case !f.Busy:
		return SettlementRetryDecision{Kind: SettlementNotRetryable, Reason: "failure is not a busy database"}
	case f.DeadlineExceeded:
		return SettlementRetryDecision{Kind: SettlementDeadline, Reason: "settlement budget ended while the database was busy"}
	case f.Attempts >= SettlementMaxAttempts:
		return SettlementRetryDecision{Kind: SettlementExhausted, Reason: fmt.Sprintf("database still busy after %d settlement attempts", f.Attempts)}
	default:
		return SettlementRetryDecision{Kind: SettlementRetry, Reason: "database was busy and the attempt was rolled back"}
	}
}

// SettledOutcomeFacts are an intent no longer dispatching: its stored
// outcome, if any, and the outcome a settlement attempt wants to record.
type SettledOutcomeFacts struct {
	Found     bool
	Stored    execution.OutcomeRecord
	Requested execution.OutcomeRecord
}

type SettledOutcomeKind string

const (
	// SettledSame: an earlier attempt recorded this outcome.
	SettledSame SettledOutcomeKind = "same"
	// SettledConflict: another outcome is recorded.
	SettledConflict SettledOutcomeKind = "conflict"
	// SettledMissing: the intent left dispatching without an outcome.
	SettledMissing SettledOutcomeKind = "missing"
)

type SettledOutcomeDecision struct {
	Kind   SettledOutcomeKind
	Reason string
}

// DecideSettledOutcome answers whether a settlement attempt that finds its
// intent already settled repeats an earlier attempt. Every field must be
// equal, the time as an instant. One difference is allowed: a stored
// divergence (advanced false, with a reason) matches a requested
// advancement, because the earlier attempt found the binding changed and
// recorded that instead.
func DecideSettledOutcome(f SettledOutcomeFacts) SettledOutcomeDecision {
	if !f.Found {
		return SettledOutcomeDecision{Kind: SettledMissing, Reason: "intent is no longer dispatching but has no outcome"}
	}
	want := f.Requested
	if f.Stored.Divergence != "" && want.Advanced && want.Divergence == "" {
		want.Advanced, want.Divergence = false, f.Stored.Divergence
	}
	stored := f.Stored
	storedAt, wantAt := stored.ObservedAt, want.ObservedAt
	stored.ObservedAt, want.ObservedAt = time.Time{}, time.Time{}
	if stored != want || !storedAt.Equal(wantAt) {
		return SettledOutcomeDecision{Kind: SettledConflict, Reason: "another outcome is already recorded"}
	}
	return SettledOutcomeDecision{Kind: SettledSame, Reason: "an earlier attempt recorded the same outcome"}
}
