package turso

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/execution/dispatch"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
)

func TestDispatchBoundsEqualTheDomainBounds(t *testing.T) {
	if schema.MaxManifestPayloadBytes != execution.MaxManifestPayloadBytes || schema.MaxIdentityBytes != execution.MaxIdentityBytes {
		t.Fatal("storage and execution bounds differ")
	}
}

// The trigger is part of the validated schema: a database without it is
// refused, so a binding lock cannot silently go missing.
func TestBindingTriggerIsValidated(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrentSchema(t, store.db)
	mustExec(t, store, "DROP TRIGGER sessions_binding_locked")
	if err := validateSchemaObjects(ctx, store.db, schema.Statements); !errors.Is(err, ErrIncompatibleDatabase) {
		t.Fatalf("validation without the trigger = %v", err)
	}
	store.Close()
	if _, err := Open(ctx, path); !errors.Is(err, ErrIncompatibleDatabase) {
		t.Fatalf("open without the trigger = %v", err)
	}
}

// Save recognizes the database's refusal as ErrBindingLocked; without an
// open intent, or changing only state and time, it writes.
func TestSaveBindingLockedByOpenIntent(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	crashed := dispatch.Service{Store: crashingStore{Store: store.Dispatch(), claim: true}, Now: func() time.Time { return dispatchNow }}
	if _, err := run(crashed, &fakeHarness{}, startCommandFor("start-1", "s1")); !errors.Is(err, errCrash) {
		t.Fatal(err)
	}
	record, err := store.Sessions().Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*sessions.Record){
		"project":         func(r *sessions.Record) { r.ProjectID = "other" },
		"run":             func(r *sessions.Record) { r.RunID = "other" },
		"attempt":         func(r *sessions.Record) { r.AttemptID = "other" },
		"assignment":      func(r *sessions.Record) { r.AssignmentID = "other" },
		"revision":        func(r *sessions.Record) { r.Revision = 4 },
		"adapter session": func(r *sessions.Record) { r.AdapterSessionID = "other" },
	} {
		changed := record
		change(&changed)
		if _, err := store.Sessions().Save(ctx, changed, record.Version); !errors.Is(err, sessions.ErrBindingLocked) {
			t.Fatalf("%s change = %v", name, err)
		}
		// The same refusal inside the unit of work rolls it back.
		err := store.WithinUnitOfWork(ctx, func(writer sessions.Writer, _ events.Appender) error {
			_, err := writer.Save(ctx, changed, record.Version)
			return err
		})
		if !errors.Is(err, sessions.ErrBindingLocked) {
			t.Fatalf("%s change in a unit of work = %v", name, err)
		}
	}
	record.State, record.UpdatedAt = "idle", dispatchNow.Add(time.Minute)
	saved, err := store.Sessions().Save(ctx, record, record.Version)
	if err != nil {
		t.Fatalf("state change with an open intent: %v", err)
	}
	// Once the intent is no longer open, the binding may change.
	mustExec(t, store, "UPDATE dispatch_intents SET state = 'invalidated', invalidated_at = ? WHERE id = 'start-1'", memoryTime)
	saved.AssignmentID = "other"
	if _, err := store.Sessions().Save(ctx, saved, saved.Version); err != nil {
		t.Fatalf("binding change without an open intent: %v", err)
	}
}

// A v1 writer on a connection opened before the migration is bound by the
// trigger too: its binding change between the claim and the outcome is
// refused by the database; the outcome then moves the revision, and the
// migrated database still validates.
func TestOldV1WriterIsRefusedBetweenClaimAndOutcome(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	v1 := createV1Database(t, path)
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	start := startCommandFor("start-v1", "v1-session")
	start.Session.AdapterSessionID, start.Current.Revision = "adapter", 1
	result := mustDispatch(t, dispatchService(store), &fakeHarness{}, start)
	steer := steerCommandFor("steer-v1", "v1-session", result.Outcome.AdapterTurnID)
	steer.Session.AdapterSessionID, steer.Current.Revision, steer.New.Revision = "adapter", 1, 2
	var v1Err error
	harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
		old, err := (sessionRepository{db: v1}).Get(ctx, "v1-session")
		if err != nil {
			v1Err = err
			return codexReceipt(r), nil
		}
		old.AssignmentID = "replaced-by-v1"
		_, v1Err = (sessionRepository{db: v1}).Save(ctx, old, old.Version)
		return codexReceipt(r), nil
	}}
	result = mustDispatch(t, dispatchService(store), harness, steer)
	if v1Err == nil || !strings.Contains(v1Err.Error(), schema.BindingLockedMessage) {
		t.Fatalf("v1 binding change = %v", v1Err)
	}
	got, err := store.Sessions().Get(ctx, "v1-session")
	if err != nil || got.AssignmentID != "assignment" || got.Revision != 2 || result.Outcome.Classification != execution.ClassAdvanced {
		t.Fatalf("session = %+v, %v; result %+v", got, err, result)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("migrated database with a refused v1 writer: %v", err)
	}
	reopened.Close()
}

// The partial unique index refuses a second open intent even from a writer
// that skips the rule.
func TestOneOpenIntentPerSession(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	insert := func(id, state string, claimed, settled, invalidated any) error {
		_, err := store.db.ExecContext(context.Background(), `INSERT INTO dispatch_intents (id, kind, session_id, session_sequence, adapter_session_id, project_id, run_id, attempt_id, assignment_id,
			expected_revision, target_revision, request_hash, memory, state, version, created_at, claimed_at, settled_at, invalidated_at)
			VALUES (?, 'start_turn', 's1', (SELECT count(*) + 1 FROM dispatch_intents), 'adapter-s1', 'project', 'run', 'attempt', 'assignment', 3, 3, ?, 'none', ?, 1, ?, ?, ?, ?)`,
			id, strings.Repeat("a", 64), state, memoryTime, claimed, settled, invalidated)
		return err
	}
	if err := insert("first", "recorded", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"recorded", "dispatching", "unknown", "diverged"} {
		var claimed, settled any
		if state != "recorded" {
			claimed = memoryTime
		}
		if state == "unknown" || state == "diverged" {
			settled = memoryTime
		}
		if insert("second-"+state, state, claimed, settled, nil) == nil {
			t.Fatalf("second open intent (%s) accepted", state)
		}
	}
	for _, state := range []string{"accepted", "rejected"} {
		if err := insert("closed-"+state, state, memoryTime, memoryTime, nil); err != nil {
			t.Fatalf("closed intent (%s) beside an open one: %v", state, err)
		}
	}
	if err := insert("closed-invalidated", "invalidated", nil, nil, memoryTime); err != nil {
		t.Fatalf("invalidated intent beside an open one: %v", err)
	}
}

// Without the trigger (a broken invariant), a proven advancement whose
// session binding changed commits as diverged with its reason, and the
// procedure reports ErrSessionBindingDiverged after the commit. A
// repetition, also after reopening, returns the same observation, reason and
// error without calling the harness.
func TestDivergedAdvancementIsRecordedAndReplayed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change string
		want   execution.Divergence
	}{
		{"binding changed", "UPDATE sessions SET assignment_id = 'other' WHERE id = 's1'", execution.DivergenceBindingChanged},
		{"adapter session changed", "UPDATE sessions SET adapter_session_id = 'other' WHERE id = 's1'", execution.DivergenceBindingChanged},
		{"revision changed", "UPDATE sessions SET revision = 9 WHERE id = 's1'", execution.DivergenceRevisionChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "dispatch.db")
			store, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			seedDispatchSession(t, store, "s1")
			turn := startAccepted(t, store, "s1")
			mustExec(t, store, "DROP TRIGGER sessions_binding_locked")
			harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
				mustExec(t, store, tc.change)
				return codexReceipt(r), nil
			}}
			command := steerCommandFor("steer-1", "s1", turn)
			result, err := run(dispatchService(store), harness, command)
			if !errors.Is(err, dispatch.ErrSessionBindingDiverged) || result.IntentState != execution.StateDiverged ||
				result.Outcome.Divergence != tc.want || result.Outcome.Advanced || result.Outcome.Classification != execution.ClassAdvanced {
				t.Fatalf("result %+v, %v", result, err)
			}
			if got := intentState(t, store, "steer-1"); got != "diverged" {
				t.Fatalf("state = %s", got)
			}
			if got := queryStrings(t, store, "SELECT CAST(advanced AS TEXT) || ' ' || divergence FROM dispatch_outcomes WHERE intent_id = 'steer-1'"); !slices.Equal(got, []string{"0 " + string(tc.want)}) {
				t.Fatalf("outcome = %v", got)
			}
			if !strings.Contains(queryStrings(t, store, "SELECT CAST(payload AS TEXT) FROM event_journal WHERE event_id = 'steer-1/settled'")[0], `"state":"diverged"`) {
				t.Fatal("settled event does not say diverged")
			}
			// Diverged is open: the partial unique index counts it.
			if got := countRows(t, store, "SELECT count(*) FROM dispatch_intents INDEXED BY dispatch_intents_one_open WHERE session_id = 's1' AND state IN ('recorded', 'dispatching', 'unknown', 'diverged')"); got != 1 {
				t.Fatalf("open intents = %d", got)
			}
			mustExec(t, store, schema.Version2Statements[slices.IndexFunc(schema.Version2Statements, func(s string) bool { return strings.Contains(s, "CREATE TRIGGER") })])
			store.Close()
			store, err = Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			again, err := run(dispatchService(store), harness, command)
			if !errors.Is(err, dispatch.ErrSessionBindingDiverged) || again.Kind != dispatch.ResultRecordedOutcome || again.Outcome != result.Outcome || harness.calls() != 1 {
				t.Fatalf("replay %+v, %v, calls %d", again, err, harness.calls())
			}
		})
	}
	t.Run("session missing", func(t *testing.T) {
		store := openMemoryStore(t)
		seedDispatchSession(t, store, "s1")
		turn := startAccepted(t, store, "s1")
		mustExec(t, store, "PRAGMA foreign_keys = OFF")
		harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
			mustExec(t, store, "DELETE FROM sessions WHERE id = 's1'")
			return codexReceipt(r), nil
		}}
		result, err := run(dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
		if !errors.Is(err, dispatch.ErrSessionBindingDiverged) || result.Outcome.Divergence != execution.DivergenceSessionMissing {
			t.Fatalf("result %+v, %v", result, err)
		}
	})
}

// A recorded unknown is replayed after reopening without a call.
func TestUnknownOutcomeIsReplayedAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dispatch.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	seedDispatchSession(t, store, "s1")
	turn := startAccepted(t, store, "s1")
	harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
		return claudeReceipt(r), nil
	}}
	first := mustDispatch(t, dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	again := mustDispatch(t, dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
	if again.Kind != dispatch.ResultRecordedOutcome || again.Outcome != first.Outcome || again.IntentState != execution.StateUnknown || harness.calls() != 1 {
		t.Fatalf("replay = %+v", again)
	}
}

// Every normalized observation, valid or invalid, is written by the real
// settlement: DecideDispatchOutcome's values always satisfy the CHECKs.
func TestEveryNormalizedObservationIsStored(t *testing.T) {
	store := openMemoryStore(t)
	pe := func(operation provider.Operation, code provider.ErrorCode, effect provider.RemoteEffect) error {
		return &provider.ProviderError{Code: code, Operation: operation, RemoteEffect: effect, Retry: "anything", Detail: "detail", Cause: errors.New("cause")}
	}
	type startCase struct {
		result func(provider.StartTurnRequest) provider.StartTurnResult
		err    error
		class  execution.Classification
		field  execution.InvalidField
	}
	starts := map[string]startCase{
		"turn started": {result: acceptedStart, class: execution.ClassTurnStarted},
		"correlation mismatch": {result: func(r provider.StartTurnRequest) provider.StartTurnResult {
			s := acceptedStart(r)
			s.Assignment.Revision = 9
			return s
		}, class: execution.ClassCorrelationMismatch},
		"invalid turn": {result: func(r provider.StartTurnRequest) provider.StartTurnResult {
			s := acceptedStart(r)
			s.Turn.AdapterTurnID = ""
			return s
		}, class: execution.ClassInvalidObservation, field: execution.InvalidTurn},
		"invalid targeting": {result: func(r provider.StartTurnRequest) provider.StartTurnResult {
			s := acceptedStart(r)
			s.TargetingStrength = "x"
			return s
		}, class: execution.ClassInvalidObservation, field: execution.InvalidTargeting},
		"invalid enforcement": {result: func(r provider.StartTurnRequest) provider.StartTurnResult {
			s := acceptedStart(r)
			s.ResultEnforcement = "none"
			return s
		}, class: execution.ClassInvalidObservation, field: execution.InvalidResultEnforcement},
		"contradictory":         {result: acceptedStart, err: errors.New("x"), class: execution.ClassInvalidObservation, field: execution.InvalidContradictory},
		"not sent":              {err: pe(provider.OperationStartTurn, provider.ErrorAuthFailed, provider.RemoteEffectNotSent), class: execution.ClassNotSent},
		"possibly sent":         {err: pe(provider.OperationStartTurn, provider.ErrorDeadline, provider.RemoteEffectPossiblySent), class: execution.ClassPossiblySent},
		"accepted no result":    {err: pe(provider.OperationStartTurn, provider.ErrorProtocolViolation, provider.RemoteEffectAccepted), class: execution.ClassAcceptedWithoutResult},
		"error correlation":     {err: pe(provider.OperationSteer, provider.ErrorProtocolViolation, provider.RemoteEffectNotSent), class: execution.ClassCorrelationMismatch},
		"invalid operation":     {err: pe("x", provider.ErrorDeadline, provider.RemoteEffectNotSent), class: execution.ClassInvalidObservation, field: execution.InvalidOperation},
		"invalid code":          {err: pe(provider.OperationStartTurn, "x", provider.RemoteEffectNotSent), class: execution.ClassInvalidObservation, field: execution.InvalidErrorCode},
		"invalid remote effect": {err: pe(provider.OperationStartTurn, provider.ErrorDeadline, "x"), class: execution.ClassInvalidObservation, field: execution.InvalidRemoteEffect},
		"other error":           {err: errors.New("pipe closed"), class: execution.ClassUnclassifiedError},
	}
	index := 0
	for name, tc := range starts {
		index++
		session := sessions.ID(fmt.Sprintf("start-%d", index))
		seedDispatchSession(t, store, session)
		harness := &fakeHarness{onStart: func(_ context.Context, r provider.StartTurnRequest) (provider.StartTurnResult, error) {
			var result provider.StartTurnResult
			if tc.result != nil {
				result = tc.result(r)
			}
			return result, tc.err
		}}
		result := mustDispatch(t, dispatchService(store), harness, startCommandFor(execution.IntentID(session), session))
		if result.Outcome.Classification != tc.class || result.Outcome.InvalidField != tc.field {
			t.Fatalf("%s = %+v", name, result.Outcome)
		}
		assertStoredOutcome(t, store, execution.IntentID(session), result.Outcome)
	}
	type steerCase struct {
		receipt func(provider.SteerRequest) provider.DeliveryReceipt
		err     error
		class   execution.Classification
		field   execution.InvalidField
	}
	change := func(apply func(*provider.DeliveryReceipt)) func(provider.SteerRequest) provider.DeliveryReceipt {
		return func(r provider.SteerRequest) provider.DeliveryReceipt {
			receipt := codexReceipt(r)
			apply(&receipt)
			return receipt
		}
	}
	steers := map[string]steerCase{
		"advanced":              {receipt: codexReceipt, class: execution.ClassAdvanced},
		"insufficient evidence": {receipt: change(func(r *provider.DeliveryReceipt) { r.DispatchedFor = provider.None[provider.AssignmentRef]() }), class: execution.ClassInsufficientEvidence},
		"transport only":        {receipt: claudeReceipt, class: execution.ClassTransportOnly},
		"queued":                {receipt: change(func(r *provider.DeliveryReceipt) { r.Disposition = provider.DeliveryQueuedByHarness }), class: execution.ClassTransportOnly},
		"rejected receipt":      {receipt: change(func(r *provider.DeliveryReceipt) { r.Disposition = provider.DeliveryRejected }), class: execution.ClassRejectedReceipt},
		"delivery unknown":      {receipt: change(func(r *provider.DeliveryReceipt) { r.Disposition = provider.DeliveryUnknown }), class: execution.ClassDeliveryUnknown},
		"dispatched for other":  {receipt: change(func(r *provider.DeliveryReceipt) { r.DispatchedFor.Value.Revision = 3 }), class: execution.ClassCorrelationMismatch},
		"invalid disposition":   {receipt: change(func(r *provider.DeliveryReceipt) { r.Disposition = "x" }), class: execution.ClassInvalidObservation, field: execution.InvalidDisposition},
		"invalid knowledge":     {receipt: change(func(r *provider.DeliveryReceipt) { r.Receipt = "x" }), class: execution.ClassInvalidObservation, field: execution.InvalidReceiptKnowledge},
		"invalid targeting":     {receipt: change(func(r *provider.DeliveryReceipt) { r.Targeting = "x" }), class: execution.ClassInvalidObservation, field: execution.InvalidTargeting},
		"receipt id None":       {receipt: change(func(r *provider.DeliveryReceipt) { r.NativeReceiptID = provider.None[provider.NativeReceiptID]() }), class: execution.ClassAdvanced},
		"receipt id 256 bytes": {receipt: change(func(r *provider.DeliveryReceipt) {
			r.NativeReceiptID = provider.Some(provider.NativeReceiptID(strings.Repeat("r", 256)))
		}), class: execution.ClassAdvanced},
		"receipt id empty": {receipt: change(func(r *provider.DeliveryReceipt) { r.NativeReceiptID = provider.Some[provider.NativeReceiptID]("") }), class: execution.ClassInvalidObservation, field: execution.InvalidNativeReceiptID},
		"receipt id 257 bytes": {receipt: change(func(r *provider.DeliveryReceipt) {
			r.NativeReceiptID = provider.Some(provider.NativeReceiptID(strings.Repeat("r", 257)))
		}), class: execution.ClassInvalidObservation, field: execution.InvalidNativeReceiptID},
		"contradictory": {receipt: codexReceipt, err: errors.New("x"), class: execution.ClassInvalidObservation, field: execution.InvalidContradictory},
		"turn mismatch error": {err: func() error {
			e := pe(provider.OperationSteer, provider.ErrorTurnLost, provider.RemoteEffectNotSent)
			e.(*provider.ProviderError).Turn = provider.Some(provider.TurnRef{AdapterTurnID: "other"})
			return e
		}(), class: execution.ClassCorrelationMismatch},
	}
	for name, tc := range steers {
		index++
		session := sessions.ID(fmt.Sprintf("steer-%d", index))
		seedDispatchSession(t, store, session)
		turn := startAccepted(t, store, session)
		harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
			var receipt provider.DeliveryReceipt
			if tc.receipt != nil {
				receipt = tc.receipt(r)
			}
			return receipt, tc.err
		}}
		id := execution.IntentID("steer-of-" + string(session))
		result := mustDispatch(t, dispatchService(store), harness, steerCommandFor(id, session, turn))
		if result.Outcome.Classification != tc.class || result.Outcome.InvalidField != tc.field {
			t.Fatalf("%s = %+v", name, result.Outcome)
		}
		assertStoredOutcome(t, store, id, result.Outcome)
		wantRevision := uint64(3)
		if tc.class == execution.ClassAdvanced {
			wantRevision = 4
		}
		if got := sessionRevision(t, store, session); got != wantRevision {
			t.Fatalf("%s: revision %d", name, got)
		}
	}
}

// assertStoredOutcome reads the outcome back through the claim's reader.
func assertStoredOutcome(t *testing.T, store *Store, id execution.IntentID, want execution.OutcomeRecord) {
	t.Helper()
	var got execution.OutcomeRecord
	var found bool
	err := store.Dispatch().WithinIntentClaim(context.Background(), func(tx dispatch.ClaimTransaction) error {
		envelope, ok, err := tx.ReadEnvelope(context.Background(), id)
		got, found = envelope.Outcome, ok && envelope.OutcomeFound
		return err
	})
	if err != nil || !found || !sameOutcome(got, want) {
		t.Fatalf("stored outcome of %s = %+v, %v; want %+v", id, got, err, want)
	}
	if state := intentState(t, store, id); state != string(want.Outcome) {
		t.Fatalf("state of %s = %s", id, state)
	}
}

// dispatchIntentRow is a valid recorded start intent row.
func dispatchIntentRow() map[string]any {
	return map[string]any{
		"id": "intent", "kind": "start_turn", "session_id": "s1", "session_sequence": 1, "adapter_session_id": "adapter-s1",
		"project_id": "project", "run_id": "run", "attempt_id": "attempt", "assignment_id": "assignment",
		"expected_revision": 3, "target_revision": 3, "expected_turn_id": nil, "delivery_mode": nil,
		"request_hash": strings.Repeat("a", 64), "memory": "none", "state": "recorded", "version": 1,
		"created_at": memoryTime, "claimed_at": nil, "settled_at": nil, "invalidated_at": nil,
	}
}

func insertRow(t *testing.T, store *Store, name string, row map[string]any) error {
	t.Helper()
	tx, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if name == "dispatch_outcomes" {
		intent := dispatchIntentRow()
		intent["state"], intent["claimed_at"], intent["settled_at"] = "unknown", memoryTime, memoryTime
		if err := insertMap(tx, "dispatch_intents", intent); err != nil {
			t.Fatal(err)
		}
	}
	return insertMap(tx, name, row)
}

func insertMap(db database, name string, row map[string]any) error {
	columns := make([]string, 0, len(row))
	for column := range row {
		columns = append(columns, column)
	}
	slices.Sort(columns)
	values := make([]any, len(columns))
	for i, column := range columns {
		values[i] = row[column]
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
	_, err := db.ExecContext(context.Background(), "INSERT INTO "+name+" ("+strings.Join(columns, ", ")+") VALUES ("+placeholders+")", values...)
	return err
}

func TestDispatchIntentConstraints(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	if err := insertRow(t, store, "dispatch_intents", dispatchIntentRow()); err != nil {
		t.Fatalf("valid intent: %v", err)
	}
	steer := func(row map[string]any) {
		row["kind"], row["target_revision"], row["expected_turn_id"], row["delivery_mode"] = "steer", 4, "turn", "active_turn"
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		ok     bool
	}{
		{"steer", steer, true},
		{"steer at the last storable revision", func(r map[string]any) {
			steer(r)
			r["expected_revision"], r["target_revision"] = int64(math.MaxInt64-1), int64(math.MaxInt64)
		}, true},
		{"steer beyond the storable revision", func(r map[string]any) {
			steer(r)
			r["expected_revision"], r["target_revision"] = int64(math.MaxInt64), int64(math.MaxInt64)
		}, false},
		{"steer skipping a revision", func(r map[string]any) { steer(r); r["target_revision"] = 5 }, false},
		{"steer without turn", func(r map[string]any) { steer(r); r["expected_turn_id"] = nil }, false},
		{"steer without mode", func(r map[string]any) { steer(r); r["delivery_mode"] = nil }, false},
		{"start with turn", func(r map[string]any) { r["expected_turn_id"] = "turn" }, false},
		{"start with other target", func(r map[string]any) { r["target_revision"] = 4 }, false},
		{"unknown kind", func(r map[string]any) { r["kind"] = "interrupt" }, false},
		{"unknown state", func(r map[string]any) { r["state"] = "sent" }, false},
		{"unknown memory", func(r map[string]any) { r["memory"] = "maybe" }, false},
		{"unknown mode", func(r map[string]any) { steer(r); r["delivery_mode"] = "later" }, false},
		{"real revision", func(r map[string]any) { r["expected_revision"], r["target_revision"] = 3.5, 3.5 }, false},
		{"overflowing revision", func(r map[string]any) { r["expected_revision"], r["target_revision"] = 9.3e18, 9.3e18 }, false},
		{"real sequence", func(r map[string]any) { r["session_sequence"] = 1.5 }, false},
		{"text revision", func(r map[string]any) { r["expected_revision"], r["target_revision"] = "three", "three" }, false},
		{"zero revision", func(r map[string]any) { r["expected_revision"], r["target_revision"] = 0, 0 }, false},
		{"zero sequence", func(r map[string]any) { r["session_sequence"] = 0 }, false},
		{"zero version", func(r map[string]any) { r["version"] = 0 }, false},
		{"short hash", func(r map[string]any) { r["request_hash"] = strings.Repeat("a", 63) }, false},
		{"no session", func(r map[string]any) { r["session_id"] = "missing" }, false},
		{"recorded with claim time", func(r map[string]any) { r["claimed_at"] = memoryTime }, false},
		{"dispatching", func(r map[string]any) { r["state"], r["claimed_at"] = "dispatching", memoryTime }, true},
		{"dispatching without claim time", func(r map[string]any) { r["state"] = "dispatching" }, false},
		{"dispatching settled", func(r map[string]any) {
			r["state"], r["claimed_at"], r["settled_at"] = "dispatching", memoryTime, memoryTime
		}, false},
		{"unknown settled", func(r map[string]any) {
			r["state"], r["claimed_at"], r["settled_at"] = "unknown", memoryTime, memoryTime
		}, true},
		{"diverged settled", func(r map[string]any) {
			r["state"], r["claimed_at"], r["settled_at"] = "diverged", memoryTime, memoryTime
		}, true},
		{"accepted without claim", func(r map[string]any) { r["state"], r["settled_at"] = "accepted", memoryTime }, false},
		{"rejected without settle time", func(r map[string]any) { r["state"], r["claimed_at"] = "rejected", memoryTime }, false},
		{"invalidated", func(r map[string]any) { r["state"], r["invalidated_at"] = "invalidated", memoryTime }, true},
		{"invalidated after claim", func(r map[string]any) {
			r["state"], r["invalidated_at"], r["claimed_at"] = "invalidated", memoryTime, memoryTime
		}, false},
		{"long id", func(r map[string]any) { r["id"] = strings.Repeat("i", 257) }, false},
		{"id at bound", func(r map[string]any) { r["id"] = strings.Repeat("i", 256) }, true},
		{"empty adapter session", func(r map[string]any) { r["adapter_session_id"] = "" }, false},
		{"long timestamp", func(r map[string]any) { r["created_at"] = strings.Repeat("t", 65) }, false},
	} {
		row := dispatchIntentRow()
		tc.change(row)
		if err := insertRow(t, store, "dispatch_intents", row); (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	for _, state := range execution.IntentStates() {
		row := dispatchIntentRow()
		row["state"] = string(state)
		switch state {
		case execution.StateDispatching:
			row["claimed_at"] = memoryTime
		case execution.StateInvalidated:
			row["invalidated_at"] = memoryTime
		case execution.StateRecorded:
		default:
			row["claimed_at"], row["settled_at"] = memoryTime, memoryTime
		}
		if err := insertRow(t, store, "dispatch_intents", row); err != nil {
			t.Fatalf("state %s: %v", state, err)
		}
	}
}

// outcomeRow is a valid receipt outcome row.
func outcomeRow() map[string]any {
	return map[string]any{
		"intent_id": "intent", "outcome": "unknown", "classification": "transport_only", "observation": "receipt",
		"invalid_field": nil, "result_enforcement": nil, "disposition": "transport_accepted", "receipt_knowledge": "no_native_receipt",
		"targeting": "unknown", "error_code": nil, "remote_effect": nil, "dispatched_for": "absent", "native_receipt_id": nil,
		"adapter_turn_id": nil, "advanced": 0, "divergence": nil, "observed_at": memoryTime,
	}
}

func TestDispatchOutcomeConstraints(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	if err := insertRow(t, store, "dispatch_outcomes", outcomeRow()); err != nil {
		t.Fatalf("valid outcome: %v", err)
	}
	start := func(r map[string]any) {
		r["outcome"], r["classification"], r["observation"] = "accepted", "turn_started", "start_result"
		r["disposition"], r["receipt_knowledge"], r["dispatched_for"] = nil, nil, nil
		r["adapter_turn_id"], r["result_enforcement"], r["targeting"] = "turn", "native_schema", "native_turn_id"
	}
	providerError := func(r map[string]any) {
		r["outcome"], r["classification"], r["observation"] = "rejected", "not_sent", "provider_error"
		r["disposition"], r["receipt_knowledge"], r["dispatched_for"], r["targeting"] = nil, nil, nil, nil
		r["error_code"], r["remote_effect"] = "stale_binding", "not_sent"
	}
	invalid := func(r map[string]any) {
		r["outcome"], r["classification"], r["observation"], r["invalid_field"] = "unknown", "invalid_observation", "invalid", "turn"
		r["disposition"], r["receipt_knowledge"], r["dispatched_for"] = nil, nil, nil
	}
	advanced := func(r map[string]any) {
		r["outcome"], r["classification"], r["receipt_knowledge"], r["dispatched_for"], r["advanced"] = "accepted", "advanced", "native_receipt_known", "matches", 1
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		ok     bool
	}{
		{"start result", start, true},
		{"start without enforcement", func(r map[string]any) { start(r); r["result_enforcement"] = nil }, false},
		{"start without turn", func(r map[string]any) { start(r); r["adapter_turn_id"] = nil }, false},
		{"start with disposition", func(r map[string]any) { start(r); r["disposition"] = "rejected" }, false},
		{"provider error", providerError, true},
		{"provider error without effect", func(r map[string]any) { providerError(r); r["remote_effect"] = nil }, false},
		{"provider error with targeting", func(r map[string]any) { providerError(r); r["targeting"] = "unknown" }, false},
		{"receipt with error code", func(r map[string]any) { r["error_code"] = "deadline" }, false},
		{"receipt without dispatched for", func(r map[string]any) { r["dispatched_for"] = nil }, false},
		{"receipt without targeting", func(r map[string]any) { r["targeting"] = nil }, false},
		{"receipt with turn", func(r map[string]any) { r["adapter_turn_id"] = "turn" }, false},
		{"other error", func(r map[string]any) {
			r["observation"], r["classification"] = "other_error", "unclassified_error"
			r["disposition"], r["receipt_knowledge"], r["dispatched_for"], r["targeting"] = nil, nil, nil, nil
		}, true},
		{"invalid with field", invalid, true},
		{"invalid keeps targeting and turn", func(r map[string]any) { invalid(r); r["targeting"], r["adapter_turn_id"] = "unknown", "turn" }, true},
		{"invalid without field", func(r map[string]any) { invalid(r); r["invalid_field"] = nil }, false},
		{"field without invalid", func(r map[string]any) { r["invalid_field"] = "turn" }, false},
		{"invalid with disposition", func(r map[string]any) { invalid(r); r["disposition"] = "rejected" }, false},
		{"invalid classified otherwise", func(r map[string]any) { invalid(r); r["classification"] = "possibly_sent" }, false},
		{"advanced", advanced, true},
		{"advanced with absent dispatched for", func(r map[string]any) { advanced(r); r["dispatched_for"] = "absent" }, false},
		{"advance without advanced class", func(r map[string]any) { r["advanced"] = 1 }, false},
		{"diverged", func(r map[string]any) { advanced(r); r["advanced"], r["divergence"] = 0, "binding_changed" }, true},
		{"diverged and advanced", func(r map[string]any) { advanced(r); r["divergence"] = "binding_changed" }, false},
		{"divergence without advanced class", func(r map[string]any) { r["divergence"] = "session_missing" }, false},
		{"unknown divergence", func(r map[string]any) { advanced(r); r["advanced"], r["divergence"] = 0, "moved" }, false},
		{"accepted transport only", func(r map[string]any) { r["outcome"] = "accepted" }, false},
		{"rejected possibly sent", func(r map[string]any) { r["outcome"], r["classification"] = "rejected", "possibly_sent" }, false},
		{"advanced flag 2", func(r map[string]any) { r["advanced"] = 2 }, false},
		{"empty receipt id", func(r map[string]any) { r["native_receipt_id"] = "" }, false},
		{"receipt id at bound", func(r map[string]any) { r["native_receipt_id"] = strings.Repeat("r", 256) }, true},
		{"receipt id over bound", func(r map[string]any) { r["native_receipt_id"] = strings.Repeat("r", 257) }, false},
		{"no intent", func(r map[string]any) { r["intent_id"] = "missing" }, false},
	} {
		row := outcomeRow()
		tc.change(row)
		if err := insertRow(t, store, "dispatch_outcomes", row); (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

// The CHECK lists are literal DDL; this keeps them equal to the domain and
// provider owners' lists.
func TestDispatchEnumerationsMatchDomain(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	accepts := func(name string, row map[string]any) bool { return insertRow(t, store, name, row) == nil }
	for _, kind := range execution.IntentKinds() {
		row := dispatchIntentRow()
		row["kind"] = string(kind)
		if kind == execution.IntentSteer {
			row["target_revision"], row["expected_turn_id"], row["delivery_mode"] = 4, "turn", "next_boundary"
		}
		if !accepts("dispatch_intents", row) {
			t.Fatalf("kind %s refused", kind)
		}
	}
	for _, presence := range execution.MemoryPresences() {
		row := dispatchIntentRow()
		row["memory"] = string(presence)
		if !accepts("dispatch_intents", row) {
			t.Fatalf("memory %s refused", presence)
		}
	}
	type column struct {
		name   string
		values []string
		row    func(map[string]any, string)
	}
	errorRow := func(r map[string]any) {
		r["observation"], r["classification"], r["outcome"] = "provider_error", "possibly_sent", "unknown"
		r["disposition"], r["receipt_knowledge"], r["dispatched_for"], r["targeting"] = nil, nil, nil, nil
		r["error_code"], r["remote_effect"] = "deadline", "possibly_sent"
	}
	for _, c := range []column{
		{"disposition", texts(execution.DeliveryDispositions()), func(r map[string]any, v string) { r["disposition"] = v }},
		{"receipt_knowledge", texts(execution.ReceiptKnowledges()), func(r map[string]any, v string) { r["receipt_knowledge"] = v }},
		{"targeting", texts(execution.TargetingStrengths()), func(r map[string]any, v string) { r["targeting"] = v }},
		{"dispatched_for", texts(execution.DispatchedFors()), func(r map[string]any, v string) { r["dispatched_for"] = v }},
		{"error_code", texts(execution.ErrorCodes()), func(r map[string]any, v string) { errorRow(r); r["error_code"] = v }},
		{"remote_effect", texts(execution.RemoteEffects()), func(r map[string]any, v string) { errorRow(r); r["remote_effect"] = v }},
		{"invalid_field", texts(execution.InvalidFields()), func(r map[string]any, v string) {
			r["observation"], r["classification"], r["invalid_field"] = "invalid", "invalid_observation", v
			r["disposition"], r["receipt_knowledge"], r["dispatched_for"] = nil, nil, nil
		}},
		{"result_enforcement", texts(execution.SchemaEnforcements()), func(r map[string]any, v string) {
			r["observation"], r["classification"], r["outcome"], r["adapter_turn_id"], r["result_enforcement"] = "start_result", "turn_started", "accepted", "turn", v
			r["disposition"], r["receipt_knowledge"], r["dispatched_for"] = nil, nil, nil
		}},
		{"divergence", texts(execution.Divergences()), func(r map[string]any, v string) {
			r["outcome"], r["classification"], r["receipt_knowledge"], r["dispatched_for"], r["divergence"] = "accepted", "advanced", "native_receipt_known", "matches", v
		}},
	} {
		for _, value := range c.values {
			row := outcomeRow()
			c.row(row, value)
			if !accepts("dispatch_outcomes", row) {
				t.Fatalf("%s %s refused", c.name, value)
			}
		}
		row := outcomeRow()
		c.row(row, "x")
		if accepts("dispatch_outcomes", row) {
			t.Fatalf("%s x accepted", c.name)
		}
	}
	for _, class := range execution.Classifications() {
		found := false
		for _, outcome := range execution.Outcomes() {
			for _, observation := range execution.ObservationKinds() {
				row := outcomeRow()
				row["outcome"], row["classification"], row["observation"] = string(outcome), string(class), string(observation)
				row["disposition"], row["receipt_knowledge"], row["dispatched_for"], row["targeting"] = nil, nil, nil, nil
				switch observation {
				case execution.ObservedReceipt:
					row["disposition"], row["receipt_knowledge"], row["dispatched_for"], row["targeting"] = "transport_accepted", "native_receipt_known", "matches", "unknown"
				case execution.ObservedStartResult:
					row["adapter_turn_id"], row["result_enforcement"], row["targeting"] = "turn", "none", "unknown"
				case execution.ObservedProviderError:
					row["error_code"], row["remote_effect"] = "deadline", "unknown"
				case execution.ObservedInvalid:
					row["invalid_field"] = "turn"
				}
				if class == execution.ClassAdvanced {
					row["advanced"] = 1
				}
				found = found || accepts("dispatch_outcomes", row)
			}
		}
		if !found {
			t.Fatalf("classification %s has no storable row", class)
		}
	}
}

func texts[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}
