package dispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Store records, claims and settles dispatch intents. It is a port of its
// own; the session unit of work is not extended.
type Store interface {
	// WithinIntentRecord runs op in one write transaction that holds the
	// database write lock from before its first read until the commit: the
	// facts, the decision and the record are atomic. It commits when op
	// returns nil and rolls back otherwise; a failed commit is rolled back.
	WithinIntentRecord(ctx context.Context, op func(RecordTransaction) error) error
	// WithinIntentClaim runs op in one such transaction.
	WithinIntentClaim(ctx context.Context, op func(ClaimTransaction) error) error
	// SettleIntent records an outcome in one transaction and repeats only a
	// transaction that failed busy and was rolled back, at most three times
	// in all and only while ctx is not done. ctx bounds acquiring a
	// connection, BEGIN, the reads and writes and the start of each attempt;
	// it is the attempt budget, not a bound on the whole wait. COMMIT,
	// ROLLBACK and discarding a connection run without cancellation, so an
	// acknowledged commit is never reported as failed, and their wall-clock
	// time is not bounded (Martin, 2026-11-16). Each attempt reads the intent
	// first: an intent already settled with the same outcome is
	// SettleAlreadySame, with another outcome ErrSettlementDiverged. A
	// failure is a *SettleFailure carrying the last attempt's commit status.
	SettleIntent(ctx context.Context, s Settlement) (SettleResult, error)
}

// RecordTransaction is valid only inside WithinIntentRecord.
type RecordTransaction interface {
	// ReadExistingIntent returns the intent with id, if any.
	ReadExistingIntent(ctx context.Context, id execution.IntentID) (execution.ExistingIntent, bool, error)
	// ReadNewIntentFacts reads the session's binding, whether it has an open
	// intent and its latest accepted start on the session's adapter session
	// and assignment identity at or below its revision.
	ReadNewIntentFacts(ctx context.Context, sessionID sessions.ID) (execution.StoredSessionFacts, error)
	// ReadRecordStates reads the project's records with recordIDs and their
	// current versions. An absent record is absent from the result.
	ReadRecordStates(ctx context.Context, projectID string, recordIDs []string) ([]memory.CurrentRecordState, error)
	// RecordIntent writes the intent at the session's next sequence, its
	// manifest with items, exclusions and payload when manifest is not nil,
	// and event, as one logical effect.
	RecordIntent(ctx context.Context, intent NewIntent, manifest *execution.NewManifest, event events.Event) error
}

// ClaimTransaction is valid only inside WithinIntentClaim.
type ClaimTransaction interface {
	// ReadEnvelope reads the intent with its manifest, payload, outcome and
	// the session's current binding.
	ReadEnvelope(ctx context.Context, id execution.IntentID) (execution.Envelope, bool, error)
	// ClaimIntent moves a recorded intent at expectedVersion to dispatching
	// and appends event. It fails with ErrIntentDiverged if the intent is not
	// recorded at that version.
	ClaimIntent(ctx context.Context, id execution.IntentID, expectedVersion int64, at time.Time, event events.Event) error
}

// NewIntent is what RecordIntent writes. Assignment is at the expected
// revision.
type NewIntent struct {
	ID             execution.IntentID
	Kind           execution.IntentKind
	SessionID      sessions.ID
	Session        provider.SessionRef
	Assignment     provider.AssignmentRef
	TargetRevision uint64
	ExpectedTurn   provider.TurnRef
	Mode           provider.DeliveryMode
	RequestHash    string
	Memory         execution.MemoryPresence
	CreatedAt      time.Time
}

// Settlement is one outcome to record. Record is DecideDispatchOutcome's,
// with Advanced set when the revision must move to TargetRevision. Event and
// DivergedEvent are built once before the first attempt and passed
// unchanged to every attempt, so a repeated append finds the stored event:
// Event when the outcome is written as decided, DivergedEvent when a proven
// advancement cannot be recorded.
type Settlement struct {
	IntentID        execution.IntentID
	ExpectedVersion int64
	SessionID       sessions.ID
	Session         provider.SessionRef
	// Assignment is the intent's binding at its expected revision.
	Assignment     provider.AssignmentRef
	TargetRevision uint64
	Record         execution.OutcomeRecord
	SettledAt      time.Time
	Event          events.Event
	DivergedEvent  events.Event
}

type SettleKind string

const (
	// SettleWritten: this call wrote the outcome.
	SettleWritten SettleKind = "settled"
	// SettleAlreadySame: an earlier attempt wrote the same outcome.
	SettleAlreadySame SettleKind = "already_settled_same"
	// SettleDiverged: the adapter proved the advancement, but the session no
	// longer had the intent's binding; the intent is diverged.
	SettleDiverged SettleKind = "diverged"
)

// SettleResult carries the stored record: with Advanced false and Divergence
// set when the intent diverged.
type SettleResult struct {
	Kind   SettleKind
	Record execution.OutcomeRecord
}

// CommitStatus is what is known about a failed settlement's last COMMIT.
type CommitStatus string

const (
	// CommitNotCommitted: nothing of the settlement took effect.
	CommitNotCommitted CommitStatus = "not_committed"
	// CommitCommitted: the outcome is durable; only the release afterwards
	// failed.
	CommitCommitted CommitStatus = "committed"
	// CommitUnknown: COMMIT failed and so did ROLLBACK, or the store did not
	// say; the outcome may be recorded.
	CommitUnknown CommitStatus = "unknown"
)

// SettleFailure is SettleIntent's error: the cause and the commit status.
type SettleFailure struct {
	Commit CommitStatus
	Err    error
}

func (e *SettleFailure) Error() string { return fmt.Sprintf("settle (commit %s): %v", e.Commit, e.Err) }

func (e *SettleFailure) Unwrap() error { return e.Err }

var (
	// ErrIntentDiverged reports that a guarded intent write found the intent
	// in another state or version than the transaction read. Nothing is
	// written.
	ErrIntentDiverged = errors.New("dispatch intent differs from the facts the transaction decided on")
	// ErrSettlementDiverged reports an intent already settled with another
	// outcome than the one being recorded.
	ErrSettlementDiverged = errors.New("dispatch intent is already settled with another outcome")
)
