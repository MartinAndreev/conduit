package turso

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/execution/dispatch"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	tursogo "turso.tech/database/tursogo"
)

// openFaultyDispatch seeds session s1 and one memory record in a fresh file
// and reopens it through a fault connector.
func openFaultyDispatch(t *testing.T) (*Store, *faultConnector, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dispatch.db")
	seeded, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	seedDispatchSession(t, seeded, "s1")
	record := humanAt(t, seeded, "Решение: паметта е локална", 0)
	if err := seeded.Close(); err != nil {
		t.Fatal(err)
	}
	db, connector := openFaulty(t, path, 5000)
	return &Store{db: db}, connector, record.RecordID
}

// Every insert of the record transaction failing leaves nothing behind:
// intent, manifest, items, exclusions, payload and event are one effect. The
// repetition then records and sends once.
func TestDispatchRecordIsAtomic(t *testing.T) {
	for _, prefix := range []string{
		"INSERT INTO dispatch_intents (",
		"INSERT INTO context_manifests (",
		"INSERT INTO context_manifest_items (",
		"INSERT INTO context_manifest_exclusions (",
		"INSERT INTO context_manifest_payloads (",
		"INSERT INTO event_journal (",
		"COMMIT",
	} {
		t.Run(prefix, func(t *testing.T) {
			store, connector, recordID := openFaultyDispatch(t)
			request, composition := composeLinked(t, store, recordID)
			composition.Exclusions = append(composition.Exclusions, compose.Exclusion{RecordID: "gone", Stage: compose.StageGather, Kind: compose.ExcludedNotFound, Reason: "linked record not found"})
			command := withMemory(startCommandFor("start-1", "s1"), request, composition)
			before := dispatchTables(t, store)
			failure := fmt.Errorf("%w: injected", tursogo.ErrTursoGeneric)
			connector.inject(prefix, failure, 1)
			harness := &fakeHarness{}
			if _, err := run(dispatchService(store), harness, command); !errors.Is(err, failure) {
				t.Fatalf("record = %v", err)
			}
			if after := dispatchTables(t, store); !mapsEqual(before, after) || harness.calls() != 0 {
				t.Fatalf("tables %v -> %v, calls %d", before, after, harness.calls())
			}
			assertPoolOutsideTransaction(t, store)
			result := mustDispatch(t, dispatchService(store), harness, command)
			if result.Kind != dispatch.ResultDispatched || harness.calls() != 1 || countRows(t, store, "SELECT count(*) FROM context_manifest_exclusions") != len(composition.Exclusions) {
				t.Fatalf("repetition = %+v", result)
			}
		})
	}
}

// injectAfterClaim runs install inside the harness call: after the claim
// committed and before the settlement, so only the settlement sees the
// faults.
func injectAfterClaim(install func()) *fakeHarness {
	return &fakeHarness{onStart: func(_ context.Context, r provider.StartTurnRequest) (provider.StartTurnResult, error) {
		install()
		return acceptedStart(r), nil
	}}
}

func assertSettledOnce(t *testing.T, store *Store, id execution.IntentID) {
	t.Helper()
	if got := intentState(t, store, id); got != "accepted" {
		t.Fatalf("state = %s", got)
	}
	if countRows(t, store, "SELECT count(*) FROM dispatch_outcomes WHERE intent_id = ?", string(id)) != 1 ||
		countRows(t, store, "SELECT count(*) FROM event_journal WHERE event_id = ?", string(id)+"/settled") != 1 {
		t.Fatal("outcome or settled event not written exactly once")
	}
}

func assertStillDispatching(t *testing.T, store *Store, id execution.IntentID) {
	t.Helper()
	if got := intentState(t, store, id); got != "dispatching" {
		t.Fatalf("state = %s", got)
	}
	if countRows(t, store, "SELECT count(*) FROM dispatch_outcomes") != 0 || countRows(t, store, "SELECT count(*) FROM event_journal WHERE kind = ?", execution.EventSettled) != 0 {
		t.Fatal("a failed settlement left an outcome or event")
	}
}

func TestSettlementBusyBeginIsRetried(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	harness := injectAfterClaim(func() { connector.inject("BEGIN IMMEDIATE", busyFault(), 1) })
	if result := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1")); result.IntentState != execution.StateAccepted {
		t.Fatalf("result = %+v", result)
	}
	if got := connector.count("BEGIN IMMEDIATE"); got != 4 {
		t.Fatalf("BEGIN IMMEDIATE = %d; record, claim and two settlement attempts expected", got)
	}
	assertSettledOnce(t, store, "start-1")
}

func TestSettlementBusyCommitRolledBackIsRetried(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	harness := injectAfterClaim(func() { connector.inject("COMMIT", busyFault(), 1) })
	if result := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1")); result.IntentState != execution.StateAccepted {
		t.Fatalf("result = %+v", result)
	}
	if connector.count("ROLLBACK") != 1 || connector.closes.Load() != 0 {
		t.Fatalf("rollbacks %d, closes %d", connector.count("ROLLBACK"), connector.closes.Load())
	}
	assertSettledOnce(t, store, "start-1")
}

// COMMIT and ROLLBACK both failing leaves the effect unknown: not retried,
// the connection is discarded, and a re-read shows what is really stored.
func TestSettlementCommitAndRollbackFailureIsNotRetried(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	harness := injectAfterClaim(func() {
		connector.inject("COMMIT", busyFault(), 1)
		connector.inject("ROLLBACK", errors.New("injected rollback failure"), 1)
	})
	_, err := run(dispatchService(store), harness, startCommandFor("start-1", "s1"))
	var settlement *dispatch.SettlementError
	if !errors.Is(err, dispatch.ErrSettlementFailed) || !errors.Is(err, tursogo.ErrTursoBusy) || !errors.As(err, &settlement) ||
		settlement.Outcome.Classification != execution.ClassTurnStarted || settlement.Commit != dispatch.CommitUnknown {
		t.Fatalf("settle = %v", err)
	}
	if connector.closes.Load() != 1 || connector.count("COMMIT") < 1 {
		t.Fatalf("closes = %d", connector.closes.Load())
	}
	assertPoolOutsideTransaction(t, store)
	assertStillDispatching(t, store, "start-1")
	if again := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1")); again.Kind != dispatch.ResultDeliveryUnknown || harness.calls() != 1 {
		t.Fatalf("repetition = %+v", again)
	}
}

// A busy failure after the budget ended starts no new attempt.
func TestSettlementDeadlineDuringReadsStopsRetrying(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	service := dispatchService(store)
	service.SettlementTimeout = 50 * time.Millisecond
	harness := injectAfterClaim(func() {
		connector.injectFault("SELECT dispatch_intents.state", fault{err: busyFault(), times: 1, before: func() { time.Sleep(100 * time.Millisecond) }})
	})
	_, err := run(service, harness, startCommandFor("start-1", "s1"))
	if !errors.Is(err, dispatch.ErrSettlementFailed) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("settle = %v", err)
	}
	if got := connector.count("BEGIN IMMEDIATE"); got != 3 {
		t.Fatalf("BEGIN IMMEDIATE = %d; one settlement attempt expected", got)
	}
	assertStillDispatching(t, store, "start-1")
}

// The budget ending while COMMIT runs does not cancel it: the acknowledged
// commit is reported.
func TestSettlementDeadlineBeforeCommitKeepsTheCommit(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	service := dispatchService(store)
	service.SettlementTimeout = 50 * time.Millisecond
	harness := injectAfterClaim(func() {
		connector.injectFault("COMMIT", fault{times: 1, before: func() { time.Sleep(100 * time.Millisecond) }})
	})
	result, err := run(service, harness, startCommandFor("start-1", "s1"))
	if err != nil || result.IntentState != execution.StateAccepted {
		t.Fatalf("result %+v, %v", result, err)
	}
	assertSettledOnce(t, store, "start-1")
}

// A COMMIT that took effect but reported busy, with a rollback that reported
// success, is retried: the next attempt reads the stored outcome as
// already_settled_same and appends no second event.
func TestSettlementRetryAfterUnconfirmedCommitFindsTheOutcome(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	harness := injectAfterClaim(func() {
		connector.injectFault("COMMIT", fault{err: busyFault(), times: 1, after: true})
		connector.injectFault("ROLLBACK", fault{times: 1, skip: true})
	})
	result := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1"))
	if result.Kind != dispatch.ResultDispatched || result.IntentState != execution.StateAccepted {
		t.Fatalf("result = %+v", result)
	}
	if got := connector.count("BEGIN IMMEDIATE"); got != 4 {
		t.Fatalf("BEGIN IMMEDIATE = %d", got)
	}
	assertSettledOnce(t, store, "start-1")
}

// At the port, a repeated settlement after a real commit is
// already_settled_same, and a different outcome is ErrSettlementDiverged.
func TestSettleIntentIsIdempotent(t *testing.T) {
	store := openMemoryStore(t)
	seedDispatchSession(t, store, "s1")
	var settlement dispatch.Settlement
	capture := capturingStore{Store: store.Dispatch(), captured: &settlement}
	mustDispatch(t, dispatch.Service{Store: capture, Now: func() time.Time { return dispatchNow }}, &fakeHarness{}, startCommandFor("start-1", "s1"))
	again, err := store.Dispatch().SettleIntent(context.Background(), settlement)
	if err != nil || again.Kind != dispatch.SettleAlreadySame || !sameOutcome(again.Record, settlement.Record) {
		t.Fatalf("again = %+v, %v", again, err)
	}
	other := settlement
	other.Record.Classification = execution.ClassCorrelationMismatch
	other.Record.Outcome = execution.OutcomeUnknown
	if _, err := store.Dispatch().SettleIntent(context.Background(), other); !errors.Is(err, dispatch.ErrSettlementDiverged) {
		t.Fatalf("other outcome = %v", err)
	}
	later := settlement
	later.Record.ObservedAt = later.Record.ObservedAt.Add(time.Second)
	if _, err := store.Dispatch().SettleIntent(context.Background(), later); !errors.Is(err, dispatch.ErrSettlementDiverged) {
		t.Fatalf("other time = %v", err)
	}
	assertSettledOnce(t, store, "start-1")
}

type capturingStore struct {
	dispatch.Store
	captured *dispatch.Settlement
}

func (s capturingStore) SettleIntent(ctx context.Context, settlement dispatch.Settlement) (dispatch.SettleResult, error) {
	*s.captured = settlement
	return s.Store.SettleIntent(ctx, settlement)
}

// A failing journal append rolls the settlement back.
func TestSettlementJournalFailureRollsBack(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	failure := fmt.Errorf("%w: injected", tursogo.ErrTursoGeneric)
	harness := injectAfterClaim(func() { connector.inject("INSERT INTO event_journal (", failure, 1) })
	_, err := run(dispatchService(store), harness, startCommandFor("start-1", "s1"))
	var settlement *dispatch.SettlementError
	if !errors.Is(err, failure) || !errors.As(err, &settlement) || settlement.Commit != dispatch.CommitNotCommitted {
		t.Fatalf("settle = %v", err)
	}
	assertStillDispatching(t, store, "start-1")
}

// A SQL failure of the session's revision UPDATE is a failure, not a
// divergence: the transaction rolls back, the intent stays dispatching and
// no outcome is written. The CAS mismatch (0 rows) case is in
// TestDivergedAdvancementIsRecordedAndReplayed.
func TestSettlementSessionUpdateFailureRollsBack(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	turn := startAccepted(t, store, "s1")
	failure := fmt.Errorf("%w: injected", tursogo.ErrTursoGeneric)
	harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
		connector.inject("UPDATE sessions", failure, 1)
		return codexReceipt(r), nil
	}}
	_, err := run(dispatchService(store), harness, steerCommandFor("steer-1", "s1", turn))
	if !errors.Is(err, failure) || errors.Is(err, dispatch.ErrSessionBindingDiverged) {
		t.Fatalf("settle = %v", err)
	}
	if got := intentState(t, store, "steer-1"); got != "dispatching" {
		t.Fatalf("state = %s", got)
	}
	if countRows(t, store, "SELECT count(*) FROM dispatch_outcomes WHERE intent_id = 'steer-1'") != 0 || sessionRevision(t, store, "s1") != 3 {
		t.Fatal("failed settlement wrote an outcome or moved the revision")
	}
}

// COMMIT took effect but reported failure and ROLLBACK failed too: the
// commit status is unknown, and indeed the outcome is already recorded. A
// repetition reads it; the harness is not called again.
func TestSettlementUnknownCommitMayAlreadyBeSettled(t *testing.T) {
	store, connector, _ := openFaultyDispatch(t)
	harness := injectAfterClaim(func() {
		connector.injectFault("COMMIT", fault{err: busyFault(), times: 1, after: true})
		connector.inject("ROLLBACK", errors.New("injected rollback failure"), 1)
	})
	_, err := run(dispatchService(store), harness, startCommandFor("start-1", "s1"))
	var settlement *dispatch.SettlementError
	if !errors.As(err, &settlement) || settlement.Commit != dispatch.CommitUnknown {
		t.Fatalf("settle = %v", err)
	}
	assertSettledOnce(t, store, "start-1")
	if again := mustDispatch(t, dispatchService(store), harness, startCommandFor("start-1", "s1")); again.Kind != dispatch.ResultRecordedOutcome || harness.calls() != 1 {
		t.Fatalf("repetition = %+v", again)
	}
}

// releaseFailingStore settles through the real store, then reports that
// releasing the connection failed after the commit.
type releaseFailingStore struct{ dispatch.Store }

var errRelease = errors.New("injected release failure")

func (s releaseFailingStore) SettleIntent(ctx context.Context, settlement dispatch.Settlement) (dispatch.SettleResult, error) {
	result, err := s.Store.SettleIntent(ctx, settlement)
	if err != nil {
		return result, err
	}
	return result, &dispatch.SettleFailure{Commit: dispatch.CommitCommitted, Err: errRelease}
}

// A committed settlement whose release failed reports what was recorded,
// not what was requested: a recorded divergence stays visible.
func TestSettlementCommittedReleaseFailureReportsTheStoredRecord(t *testing.T) {
	t.Run("not diverged", func(t *testing.T) {
		store := openMemoryStore(t)
		seedDispatchSession(t, store, "s1")
		service := dispatch.Service{Store: releaseFailingStore{store.Dispatch()}, Now: func() time.Time { return dispatchNow }}
		_, err := run(service, &fakeHarness{}, startCommandFor("start-1", "s1"))
		var settlement *dispatch.SettlementError
		if !errors.As(err, &settlement) || settlement.Commit != dispatch.CommitCommitted || !errors.Is(err, errRelease) ||
			settlement.Outcome.Classification != execution.ClassTurnStarted || errors.Is(err, dispatch.ErrSessionBindingDiverged) {
			t.Fatalf("settle = %v", err)
		}
		assertSettledOnce(t, store, "start-1")
	})
	t.Run("diverged", func(t *testing.T) {
		store := openMemoryStore(t)
		seedDispatchSession(t, store, "s1")
		turn := startAccepted(t, store, "s1")
		mustExec(t, store, "DROP TRIGGER sessions_binding_locked")
		harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
			mustExec(t, store, "UPDATE sessions SET assignment_id = 'other' WHERE id = 's1'")
			return codexReceipt(r), nil
		}}
		service := dispatch.Service{Store: releaseFailingStore{store.Dispatch()}, Now: func() time.Time { return dispatchNow }}
		_, err := run(service, harness, steerCommandFor("steer-1", "s1", turn))
		var settlement *dispatch.SettlementError
		if !errors.As(err, &settlement) || settlement.Commit != dispatch.CommitCommitted || !errors.Is(err, dispatch.ErrSessionBindingDiverged) ||
			settlement.Outcome.Divergence != execution.DivergenceBindingChanged || settlement.Outcome.Advanced {
			t.Fatalf("settle = %v", err)
		}
		if got := intentState(t, store, "steer-1"); got != "diverged" {
			t.Fatalf("state = %s", got)
		}
	})
}
