package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/execution/dispatch"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/model"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	"github.com/MartinAndreev/conduit/internal/storage/turso/rules"
	jet "github.com/go-jet/jet/v2/sqlite"
	tursogo "turso.tech/database/tursogo"
)

// Dispatch returns the dispatch intent port: the atomic record of intent,
// manifest and payload, the claim and the settlement.
func (s *Store) Dispatch() dispatch.Store { return dispatchStore{db: s.db} }

var _ dispatch.Store = dispatchStore{}

type dispatchStore struct{ db *sql.DB }

// WithinIntentRecord runs op in one memory write transaction
// (withMemoryWrite): BEGIN IMMEDIATE before the first read.
func (s dispatchStore) WithinIntentRecord(ctx context.Context, op func(dispatch.RecordTransaction) error) error {
	return withMemoryWrite(ctx, s.db, "dispatch intent record", func(conn *sql.Conn) error {
		return op(dispatchTx{db: conn})
	})
}

// WithinIntentClaim runs op in one memory write transaction.
func (s dispatchStore) WithinIntentClaim(ctx context.Context, op func(dispatch.ClaimTransaction) error) error {
	return withMemoryWrite(ctx, s.db, "dispatch intent claim", func(conn *sql.Conn) error {
		return op(dispatchTx{db: conn})
	})
}

// SettleIntent repeats the settlement transaction while
// rules.DecideSettlementRetry says so. Each attempt is one writeAttempt;
// its failure, cleanup and commit status are separate facts.
func (s dispatchStore) SettleIntent(ctx context.Context, settlement dispatch.Settlement) (dispatch.SettleResult, error) {
	for attempts := 1; ; attempts++ {
		var result dispatch.SettleResult
		failure, cleanup, commit := writeAttempt(ctx, s.db, "dispatch settlement", func(conn *sql.Conn) error {
			var err error
			result, err = settleIn(ctx, dispatchTx{db: conn}, settlement)
			return err
		})
		if commit == rules.SettlementCommitted {
			// The outcome is durable; a failure to release the connection
			// afterwards is reported with it.
			if cleanup != nil {
				return result, &dispatch.SettleFailure{Commit: dispatch.CommitCommitted, Err: cleanup}
			}
			return result, nil
		}
		err := errors.Join(failure, cleanup)
		decision := rules.DecideSettlementRetry(rules.SettlementRetryFacts{
			Busy:             errors.Is(failure, tursogo.ErrTursoBusy),
			CleanupFailed:    cleanup != nil,
			Commit:           commit,
			DeadlineExceeded: ctx.Err() != nil,
			Attempts:         attempts,
		})
		// Earlier attempts were retried only after a rollback, so the last
		// attempt's COMMIT status is the settlement's.
		status := dispatch.CommitNotCommitted
		if commit == rules.SettlementCommitFailedUnknown {
			status = dispatch.CommitUnknown
		}
		switch decision.Kind {
		case rules.SettlementRetry:
			continue
		case rules.SettlementDeadline:
			err = errors.Join(fmt.Errorf("settle dispatch intent: %s", decision.Reason), err, ctx.Err())
		case rules.SettlementExhausted:
			err = fmt.Errorf("settle dispatch intent: %s: %w", decision.Reason, err)
		}
		return dispatch.SettleResult{}, &dispatch.SettleFailure{Commit: status, Err: err}
	}
}

// writeAttempt runs one write transaction on a pinned connection, as
// migrateSchemaOnce does, and reports separately the attempt's failure, the
// failure to end the transaction or release the connection, and how far its
// COMMIT got. ctx bounds the connection, BEGIN IMMEDIATE and op; COMMIT,
// ROLLBACK and discarding the connection run without cancellation.
func writeAttempt(ctx context.Context, db *sql.DB, purpose string, op func(*sql.Conn) error) (failure, cleanup error, commit rules.SettlementCommit) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open %s connection: %w", purpose, err), nil, rules.SettlementCommitNotReached
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin %s: %w", purpose, err), conn.Close(), rules.SettlementCommitNotReached
	}
	if err := op(conn); err != nil {
		return err, rollbackAndRelease(conn, purpose), rules.SettlementCommitNotReached
	}
	if _, err := conn.ExecContext(context.WithoutCancel(ctx), "COMMIT"); err != nil {
		failure = fmt.Errorf("commit %s: %w", purpose, err)
		if cleanup = rollbackAndRelease(conn, purpose); cleanup != nil {
			return failure, cleanup, rules.SettlementCommitFailedUnknown
		}
		return failure, nil, rules.SettlementCommitFailedRolledBack
	}
	return nil, conn.Close(), rules.SettlementCommitted
}

type dispatchTx struct{ db database }

var openStates = []execution.IntentState{execution.StateRecorded, execution.StateDispatching, execution.StateUnknown, execution.StateDiverged}

func stateList(states []execution.IntentState) []jet.Expression {
	values := make([]string, len(states))
	for i, state := range states {
		values[i] = string(state)
	}
	return stringList(values)
}

// ReadExistingIntent reads the intent's request hash and its manifest hash.
func (t dispatchTx) ReadExistingIntent(ctx context.Context, id execution.IntentID) (execution.ExistingIntent, bool, error) {
	intents, manifests := table.DispatchIntents, table.ContextManifests
	query, args := jet.SELECT(intents.RequestHash, manifests.ManifestHash).
		FROM(intents.LEFT_JOIN(manifests, manifests.IntentID.EQ(intents.ID))).
		WHERE(intents.ID.EQ(jet.String(string(id)))).Sql()
	var existing execution.ExistingIntent
	var manifestHash sql.NullString
	err := t.db.QueryRowContext(ctx, query, args...).Scan(&existing.RequestHash, &manifestHash)
	if errors.Is(err, sql.ErrNoRows) {
		return execution.ExistingIntent{}, false, nil
	}
	if err != nil {
		return execution.ExistingIntent{}, false, fmt.Errorf("read dispatch intent: %w", err)
	}
	existing.HasManifest, existing.ManifestHash = manifestHash.Valid, manifestHash.String
	return existing, true, nil
}

// readSessionBinding reads a session's binding; a missing session is false.
func (t dispatchTx) readSessionBinding(ctx context.Context, sessionID sessions.ID) (execution.SessionBinding, bool, error) {
	s := table.Sessions
	query, args := jet.SELECT(s.ProjectID, s.RunID, s.AttemptID, s.AssignmentID, s.Revision, s.AdapterSessionID).
		FROM(s).WHERE(s.ID.EQ(jet.String(string(sessionID)))).Sql()
	var b execution.SessionBinding
	a := &b.Assignment
	err := t.db.QueryRowContext(ctx, query, args...).Scan(&a.ProjectID, &a.RunID, &a.AttemptID, &a.AssignmentID, &a.Revision, &b.AdapterSessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return execution.SessionBinding{}, false, nil
	}
	if err != nil {
		return execution.SessionBinding{}, false, fmt.Errorf("read session binding: %w", err)
	}
	return b, true, nil
}

// ReadNewIntentFacts reads the binding, whether an open intent exists and
// the latest accepted start on the session's adapter session and assignment
// identity at or below its revision.
func (t dispatchTx) ReadNewIntentFacts(ctx context.Context, sessionID sessions.ID) (execution.StoredSessionFacts, error) {
	binding, found, err := t.readSessionBinding(ctx, sessionID)
	if err != nil || !found {
		return execution.StoredSessionFacts{}, err
	}
	facts := execution.StoredSessionFacts{SessionFound: true, Session: binding}
	intents, outcomes := table.DispatchIntents, table.DispatchOutcomes
	query, args := jet.SELECT(jet.COUNT(jet.STAR)).FROM(intents).
		WHERE(intents.SessionID.EQ(jet.String(string(sessionID))).AND(intents.State.IN(stateList(openStates)...))).Sql()
	var open int64
	if err := t.db.QueryRowContext(ctx, query, args...).Scan(&open); err != nil {
		return execution.StoredSessionFacts{}, fmt.Errorf("read open dispatch intents: %w", err)
	}
	facts.OpenIntent = open > 0
	a := binding.Assignment
	query, args = jet.SELECT(intents.SessionSequence, intents.AdapterSessionID, intents.ProjectID, intents.RunID, intents.AttemptID,
		intents.AssignmentID, intents.ExpectedRevision, outcomes.AdapterTurnID, intents.CreatedAt).
		FROM(intents.INNER_JOIN(outcomes, outcomes.IntentID.EQ(intents.ID))).
		WHERE(intents.SessionID.EQ(jet.String(string(sessionID))).
			AND(intents.Kind.EQ(jet.String(string(execution.IntentStartTurn)))).
			AND(intents.State.EQ(jet.String(string(execution.StateAccepted)))).
			AND(intents.AdapterSessionID.EQ(jet.String(string(binding.AdapterSessionID)))).
			AND(intents.ProjectID.EQ(jet.String(string(a.ProjectID)))).
			AND(intents.RunID.EQ(jet.String(string(a.RunID)))).
			AND(intents.AttemptID.EQ(jet.String(string(a.AttemptID)))).
			AND(intents.AssignmentID.EQ(jet.String(string(a.AssignmentID)))).
			AND(intents.ExpectedRevision.LT_EQ(jet.Int(int64(a.Revision))))).
		ORDER_BY(intents.SessionSequence.DESC()).LIMIT(1).Sql()
	var start execution.AcceptedStart
	var turn sql.NullString
	var created string
	sa := &start.Assignment
	err = t.db.QueryRowContext(ctx, query, args...).Scan(&start.SessionSequence, &start.AdapterSessionID, &sa.ProjectID, &sa.RunID,
		&sa.AttemptID, &sa.AssignmentID, &sa.Revision, &turn, &created)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return facts, nil
	case err != nil:
		return execution.StoredSessionFacts{}, fmt.Errorf("read accepted start: %w", err)
	}
	start.AdapterTurnID = provider.AdapterTurnID(turn.String)
	if start.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return execution.StoredSessionFacts{}, fmt.Errorf("accepted start creation time: %w", ErrInvalidStoredTimestamp)
	}
	facts.AcceptedStarts = []execution.AcceptedStart{start}
	return facts, nil
}

// ReadRecordStates reads the project's records with their current versions,
// in chunks.
func (t dispatchTx) ReadRecordStates(ctx context.Context, projectID string, recordIDs []string) ([]memory.CurrentRecordState, error) {
	records, versions := table.MemoryRecords, table.MemoryRecordVersions
	var states []memory.CurrentRecordState
	for chunk := range slices.Chunk(recordIDs, memoryIDChunk) {
		query, args := jet.SELECT(records.ID, records.Status, records.CurrentVersionID, records.Version, versions.ContentDeletedAt.IS_NOT_NULL()).
			FROM(records.INNER_JOIN(versions, versions.ID.EQ(records.CurrentVersionID).AND(versions.RecordID.EQ(records.ID)))).
			WHERE(records.ProjectID.EQ(jet.String(projectID)).AND(records.ID.IN(stringList(chunk)...))).Sql()
		rows, err := t.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("read memory record states: %w", err)
		}
		for rows.Next() {
			var s memory.CurrentRecordState
			if err := rows.Scan(&s.RecordID, &s.Status, &s.CurrentVersionID, &s.RecordVersion, &s.ContentDeleted); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan memory record state: %w", ErrUndecodableMemoryRow)
			}
			states = append(states, s)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("iterate memory record states: %w", err)
		}
	}
	return states, nil
}

// RecordIntent writes the intent at the session's next sequence, then the
// manifest, its items, exclusions and payload, then the event. The partial
// unique index refuses a second open intent of the session even from a
// writer that skipped the rule.
func (t dispatchTx) RecordIntent(ctx context.Context, intent dispatch.NewIntent, manifest *execution.NewManifest, event events.Event) error {
	intents := table.DispatchIntents
	query, args := jet.SELECT(jet.MAX(intents.SessionSequence)).
		FROM(intents).WHERE(intents.SessionID.EQ(jet.String(string(intent.SessionID)))).Sql()
	var maximum sql.NullInt64
	if err := t.db.QueryRowContext(ctx, query, args...).Scan(&maximum); err != nil {
		return fmt.Errorf("allocate dispatch session sequence: %w", err)
	}
	sequence := maximum.Int64 + 1
	a := intent.Assignment
	row := model.DispatchIntents{
		ID: string(intent.ID), Kind: string(intent.Kind), SessionID: string(intent.SessionID), SessionSequence: sequence,
		AdapterSessionID: string(intent.Session.AdapterSessionID),
		ProjectID:        string(a.ProjectID), RunID: string(a.RunID), AttemptID: string(a.AttemptID), AssignmentID: string(a.AssignmentID),
		ExpectedRevision: int64(a.Revision), TargetRevision: int64(intent.TargetRevision),
		ExpectedTurnID: present(string(intent.ExpectedTurn.AdapterTurnID)), DeliveryMode: present(string(intent.Mode)),
		RequestHash: intent.RequestHash, Memory: string(intent.Memory), State: string(execution.StateRecorded), Version: 1,
		CreatedAt: formatStoredTime(intent.CreatedAt),
	}
	if err := execStatement(ctx, t.db, "insert dispatch intent", intents.INSERT(intents.AllColumns).MODEL(row)); err != nil {
		return err
	}
	if manifest != nil {
		if err := t.insertManifest(ctx, intent, *manifest); err != nil {
			return err
		}
	}
	return t.appendEvent(ctx, event)
}

// insertManifest writes the manifest with the intent's ID and its target
// assignment.
func (t dispatchTx) insertManifest(ctx context.Context, intent dispatch.NewIntent, m execution.NewManifest) error {
	manifests, items, exclusions, payloads := table.ContextManifests, table.ContextManifestItems, table.ContextManifestExclusions, table.ContextManifestPayloads
	id, a := string(intent.ID), intent.Assignment
	row := model.ContextManifests{
		ID: id, ProjectID: string(a.ProjectID), RunID: string(a.RunID), AttemptID: string(a.AttemptID), AssignmentID: string(a.AssignmentID),
		Revision: int64(intent.TargetRevision), IntentID: id, Composition: string(m.Composition), PayloadHash: m.PayloadHash,
		ManifestHash: m.ManifestHash, RedactionPolicyVersion: present(m.RedactionPolicyVersion), SearchStatus: string(m.SearchStatus),
		CreatedAt: formatStoredTime(intent.CreatedAt),
	}
	if err := execStatement(ctx, t.db, "insert context manifest", manifests.INSERT(manifests.AllColumns).MODEL(row)); err != nil {
		return err
	}
	for start := 0; start < len(m.Items); start += memoryIDChunk {
		chunk := m.Items[start:min(start+memoryIDChunk, len(m.Items))]
		rows := make([]model.ContextManifestItems, len(chunk))
		for i, item := range chunk {
			rows[i] = model.ContextManifestItems{
				ManifestID: id, Ordinal: int64(start + i), SourceIdentity: item.RecordID, SourceVersion: item.VersionID,
				Mode: string(item.Mode), Reason: item.Reason, Trust: present(string(item.Trust)),
			}
		}
		if err := execStatement(ctx, t.db, "insert context manifest items", items.INSERT(items.AllColumns).MODELS(rows)); err != nil {
			return err
		}
	}
	for start := 0; start < len(m.Exclusions); start += memoryIDChunk {
		chunk := m.Exclusions[start:min(start+memoryIDChunk, len(m.Exclusions))]
		rows := make([]model.ContextManifestExclusions, len(chunk))
		for i, x := range chunk {
			rows[i] = model.ContextManifestExclusions{
				ManifestID: id, Ordinal: int64(start + i), RecordID: present(x.RecordID), VersionID: present(x.VersionID),
				IdentityWithheld: flag(x.IdentityWithheld), Mandatory: flag(x.Mandatory),
				Stage: string(x.Stage), Kind: string(x.Kind), Reason: x.Reason,
			}
		}
		if err := execStatement(ctx, t.db, "insert context manifest exclusions", exclusions.INSERT(exclusions.AllColumns).MODELS(rows)); err != nil {
			return err
		}
	}
	if m.Composition != execution.CompositionComposed {
		return nil
	}
	payload := model.ContextManifestPayloads{ManifestID: id, Content: &m.Payload, Status: string(memory.PayloadPresent)}
	return execStatement(ctx, t.db, "insert context manifest payload", payloads.INSERT(payloads.AllColumns).MODEL(payload))
}

func flag(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

// appendEvent appends in the caller's transaction. The same event already
// stored is not an error; different content with its ID is.
func (t dispatchTx) appendEvent(ctx context.Context, event events.Event) error {
	if _, err := (eventRepository{db: t.db}).Append(ctx, event); err != nil {
		return fmt.Errorf("append %s event: %w", event.Kind, err)
	}
	return nil
}

// ReadEnvelope reads the intent, its manifest, payload and outcome and the
// session's current binding.
func (t dispatchTx) ReadEnvelope(ctx context.Context, id execution.IntentID) (execution.Envelope, bool, error) {
	i := table.DispatchIntents
	query, args := jet.SELECT(i.Kind, i.State, i.Version, i.SessionID, i.AdapterSessionID, i.ProjectID, i.RunID, i.AttemptID,
		i.AssignmentID, i.ExpectedRevision, i.TargetRevision, i.ExpectedTurnID, i.DeliveryMode, i.RequestHash, i.Memory).
		FROM(i).WHERE(i.ID.EQ(jet.String(string(id)))).Sql()
	e := execution.Envelope{IntentID: id}
	a := &e.Assignment
	var turn, mode sql.NullString
	err := t.db.QueryRowContext(ctx, query, args...).Scan(&e.Kind, &e.State, &e.Version, &e.SessionID, &e.Session.AdapterSessionID,
		&a.ProjectID, &a.RunID, &a.AttemptID, &a.AssignmentID, &a.Revision, &e.TargetRevision, &turn, &mode, &e.RequestHash, &e.Memory)
	if errors.Is(err, sql.ErrNoRows) {
		return execution.Envelope{}, false, nil
	}
	if err != nil {
		return execution.Envelope{}, false, fmt.Errorf("read dispatch intent: %w", err)
	}
	e.ExpectedTurn.AdapterTurnID, e.Mode = provider.AdapterTurnID(turn.String), provider.DeliveryMode(mode.String)
	if err := t.readEnvelopeManifest(ctx, &e); err != nil {
		return execution.Envelope{}, false, err
	}
	e.Outcome, e.OutcomeFound, err = t.readOutcome(ctx, id)
	if err != nil {
		return execution.Envelope{}, false, err
	}
	e.CurrentSession, e.SessionFound, err = t.readSessionBinding(ctx, e.SessionID)
	if err != nil {
		return execution.Envelope{}, false, err
	}
	return e, true, nil
}

func (t dispatchTx) readEnvelopeManifest(ctx context.Context, e *execution.Envelope) error {
	m, p := table.ContextManifests, table.ContextManifestPayloads
	query, args := jet.SELECT(m.Composition, m.PayloadHash, m.ManifestHash, p.Status, p.Content).
		FROM(m.LEFT_JOIN(p, p.ManifestID.EQ(m.ID))).
		WHERE(m.IntentID.EQ(jet.String(string(e.IntentID)))).Sql()
	var status, content sql.NullString
	err := t.db.QueryRowContext(ctx, query, args...).Scan(&e.Composition, &e.PayloadHash, &e.ManifestHash, &status, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read context manifest: %w", err)
	}
	e.ManifestFound = true
	e.PayloadFound, e.PayloadStatus, e.PayloadText = status.Valid, memory.PayloadStatus(status.String), content.String
	return nil
}

// readOutcome reads an intent's stored outcome.
func (t dispatchTx) readOutcome(ctx context.Context, id execution.IntentID) (execution.OutcomeRecord, bool, error) {
	o := table.DispatchOutcomes
	query, args := jet.SELECT(o.Outcome, o.Classification, o.Observation, o.InvalidField, o.ResultEnforcement, o.Disposition,
		o.ReceiptKnowledge, o.Targeting, o.ErrorCode, o.RemoteEffect, o.DispatchedFor, o.NativeReceiptID, o.AdapterTurnID,
		o.Advanced, o.Divergence, o.ObservedAt).
		FROM(o).WHERE(o.IntentID.EQ(jet.String(string(id)))).Sql()
	var r execution.OutcomeRecord
	var invalid, enforcement, disposition, knowledge, targeting, code, effect, dispatchedFor, receipt, turn, divergence sql.NullString
	var advanced int64
	var observed string
	err := t.db.QueryRowContext(ctx, query, args...).Scan(&r.Outcome, &r.Classification, &r.Observation, &invalid, &enforcement,
		&disposition, &knowledge, &targeting, &code, &effect, &dispatchedFor, &receipt, &turn, &advanced, &divergence, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		return execution.OutcomeRecord{}, false, nil
	}
	if err != nil {
		return execution.OutcomeRecord{}, false, fmt.Errorf("read dispatch outcome: %w", err)
	}
	r.InvalidField = execution.InvalidField(invalid.String)
	r.ResultEnforcement = provider.SchemaEnforcement(enforcement.String)
	r.Disposition = provider.DeliveryDisposition(disposition.String)
	r.ReceiptKnowledge = provider.ReceiptKnowledge(knowledge.String)
	r.Targeting = provider.TargetingStrength(targeting.String)
	r.ErrorCode = provider.ErrorCode(code.String)
	r.RemoteEffect = provider.RemoteEffect(effect.String)
	r.DispatchedFor = execution.DispatchedFor(dispatchedFor.String)
	r.NativeReceiptID = provider.NativeReceiptID(receipt.String)
	r.AdapterTurnID = provider.AdapterTurnID(turn.String)
	r.Advanced = advanced == 1
	r.Divergence = execution.Divergence(divergence.String)
	if r.ObservedAt, err = time.Parse(time.RFC3339Nano, observed); err != nil {
		return execution.OutcomeRecord{}, false, fmt.Errorf("dispatch outcome time: %w", ErrInvalidStoredTimestamp)
	}
	return r, true, nil
}

// ClaimIntent moves the intent from recorded at expectedVersion to
// dispatching.
func (t dispatchTx) ClaimIntent(ctx context.Context, id execution.IntentID, expectedVersion int64, at time.Time, event events.Event) error {
	i := table.DispatchIntents
	claim := i.UPDATE(i.State, i.ClaimedAt, i.Version).
		SET(jet.String(string(execution.StateDispatching)), jet.String(formatStoredTime(at)), i.Version.ADD(jet.Int(1))).
		WHERE(i.ID.EQ(jet.String(string(id))).
			AND(i.State.EQ(jet.String(string(execution.StateRecorded)))).
			AND(i.Version.EQ(jet.Int(expectedVersion))))
	if err := execExpectingRows(ctx, t.db, "claim dispatch intent", claim, 1, dispatch.ErrIntentDiverged); err != nil {
		return err
	}
	return t.appendEvent(ctx, event)
}

// settleIn is one settlement attempt. It reads the intent first, so an
// attempt after an unconfirmed but real commit finds the stored outcome.
func settleIn(ctx context.Context, t dispatchTx, s dispatch.Settlement) (dispatch.SettleResult, error) {
	i := table.DispatchIntents
	query, args := jet.SELECT(i.State, i.Version).FROM(i).WHERE(i.ID.EQ(jet.String(string(s.IntentID)))).Sql()
	var state execution.IntentState
	var version int64
	err := t.db.QueryRowContext(ctx, query, args...).Scan(&state, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return dispatch.SettleResult{}, fmt.Errorf("settle dispatch intent: %w", dispatch.ErrIntentDiverged)
	}
	if err != nil {
		return dispatch.SettleResult{}, fmt.Errorf("read dispatch intent: %w", err)
	}
	if state != execution.StateDispatching {
		return alreadySettled(ctx, t, s)
	}
	if version != s.ExpectedVersion {
		return dispatch.SettleResult{}, fmt.Errorf("settle dispatch intent: %w", dispatch.ErrIntentDiverged)
	}
	record := s.Record
	settledState := execution.IntentState(record.Outcome)
	if err := t.settleIntentRow(ctx, s, execution.StateDispatching, settledState, version); err != nil {
		return dispatch.SettleResult{}, err
	}
	if !record.Advanced {
		return t.writeOutcome(ctx, s, record, s.Event, dispatch.SettleWritten)
	}
	// The intent is no longer open, so sessions_binding_locked lets the
	// revision move. The guarded UPDATE's row count is a fact, not a failure:
	// 0 rows means the binding invariant was broken and the intent diverges.
	moved, err := t.advanceSession(ctx, s)
	if err != nil {
		return dispatch.SettleResult{}, err
	}
	if moved {
		return t.writeOutcome(ctx, s, record, s.Event, dispatch.SettleWritten)
	}
	divergence, err := t.divergence(ctx, s)
	if err != nil {
		return dispatch.SettleResult{}, err
	}
	if err := t.settleIntentRow(ctx, s, settledState, execution.StateDiverged, version+1); err != nil {
		return dispatch.SettleResult{}, err
	}
	record.Advanced, record.Divergence = false, divergence
	return t.writeOutcome(ctx, s, record, s.DivergedEvent, dispatch.SettleDiverged)
}

// settleIntentRow moves the intent from one state at version to another,
// guarded by both.
func (t dispatchTx) settleIntentRow(ctx context.Context, s dispatch.Settlement, from, to execution.IntentState, version int64) error {
	i := table.DispatchIntents
	update := i.UPDATE(i.State, i.SettledAt, i.Version).
		SET(jet.String(string(to)), jet.String(formatStoredTime(s.SettledAt)), i.Version.ADD(jet.Int(1))).
		WHERE(i.ID.EQ(jet.String(string(s.IntentID))).
			AND(i.State.EQ(jet.String(string(from)))).
			AND(i.Version.EQ(jet.Int(version))))
	return execExpectingRows(ctx, t.db, "settle dispatch intent", update, 1, dispatch.ErrIntentDiverged)
}

// advanceSession moves the session's revision to the target if it still has
// the intent's full binding at the expected revision, and reports whether it
// did. The session's optimistic version is bumped, so a concurrent unit of
// work that read the old record fails with its version conflict.
func (t dispatchTx) advanceSession(ctx context.Context, s dispatch.Settlement) (bool, error) {
	sessionTable, a := table.Sessions, s.Assignment
	update := sessionTable.UPDATE(sessionTable.Revision, sessionTable.Version, sessionTable.UpdatedAt).
		SET(jet.Int(int64(s.TargetRevision)), sessionTable.Version.ADD(jet.Int(1)), jet.String(s.SettledAt.UTC().Format(time.RFC3339Nano))).
		WHERE(sessionTable.ID.EQ(jet.String(string(s.SessionID))).
			AND(sessionTable.ProjectID.EQ(jet.String(string(a.ProjectID)))).
			AND(sessionTable.RunID.EQ(jet.String(string(a.RunID)))).
			AND(sessionTable.AttemptID.EQ(jet.String(string(a.AttemptID)))).
			AND(sessionTable.AssignmentID.EQ(jet.String(string(a.AssignmentID)))).
			AND(sessionTable.AdapterSessionID.EQ(jet.String(string(s.Session.AdapterSessionID)))).
			AND(sessionTable.Revision.EQ(jet.Int(int64(a.Revision)))))
	query, args := update.Sql()
	result, err := t.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("advance session revision: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("advance session revision: inspect result: %w", err)
	}
	return changed == 1, nil
}

// divergence names what the re-read session shows instead of the intent's
// binding.
func (t dispatchTx) divergence(ctx context.Context, s dispatch.Settlement) (execution.Divergence, error) {
	binding, found, err := t.readSessionBinding(ctx, s.SessionID)
	switch {
	case err != nil:
		return "", err
	case !found:
		return execution.DivergenceSessionMissing, nil
	case binding.Assignment.SameIdentity(s.Assignment) && binding.AdapterSessionID == s.Session.AdapterSessionID:
		return execution.DivergenceRevisionChanged, nil
	default:
		return execution.DivergenceBindingChanged, nil
	}
}

func (t dispatchTx) writeOutcome(ctx context.Context, s dispatch.Settlement, r execution.OutcomeRecord, event events.Event, kind dispatch.SettleKind) (dispatch.SettleResult, error) {
	o := table.DispatchOutcomes
	row := model.DispatchOutcomes{
		IntentID: string(s.IntentID), Outcome: string(r.Outcome), Classification: string(r.Classification), Observation: string(r.Observation),
		InvalidField: present(string(r.InvalidField)), ResultEnforcement: present(string(r.ResultEnforcement)),
		Disposition: present(string(r.Disposition)), ReceiptKnowledge: present(string(r.ReceiptKnowledge)),
		Targeting: present(string(r.Targeting)), ErrorCode: present(string(r.ErrorCode)), RemoteEffect: present(string(r.RemoteEffect)),
		DispatchedFor: present(string(r.DispatchedFor)), NativeReceiptID: present(string(r.NativeReceiptID)),
		AdapterTurnID: present(string(r.AdapterTurnID)), Advanced: flag(r.Advanced), Divergence: present(string(r.Divergence)),
		ObservedAt: formatStoredTime(r.ObservedAt),
	}
	if err := execStatement(ctx, t.db, "insert dispatch outcome", o.INSERT(o.AllColumns).MODEL(row)); err != nil {
		return dispatch.SettleResult{}, err
	}
	if err := t.appendEvent(ctx, event); err != nil {
		return dispatch.SettleResult{}, err
	}
	return dispatch.SettleResult{Kind: kind, Record: r}, nil
}

// alreadySettled reads an earlier attempt's stored outcome and lets
// rules.DecideSettledOutcome compare it with this one.
func alreadySettled(ctx context.Context, t dispatchTx, s dispatch.Settlement) (dispatch.SettleResult, error) {
	stored, found, err := t.readOutcome(ctx, s.IntentID)
	if err != nil {
		return dispatch.SettleResult{}, err
	}
	decision := rules.DecideSettledOutcome(rules.SettledOutcomeFacts{Found: found, Stored: stored, Requested: s.Record})
	if decision.Kind != rules.SettledSame {
		return dispatch.SettleResult{}, fmt.Errorf("settle dispatch intent: %s: %w", decision.Reason, dispatch.ErrSettlementDiverged)
	}
	return dispatch.SettleResult{Kind: dispatch.SettleAlreadySame, Record: stored}, nil
}
