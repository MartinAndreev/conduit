package turso

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/execution"
	"github.com/MartinAndreev/conduit/internal/execution/dispatch"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const dispatchRaceRounds = 12

// Two stores dispatching the same command serialize on the write lock: one
// records and sends, the other finds the intent and never sends.
func TestTwoStoresDispatchOneIntentOnce(t *testing.T) {
	first, second := openRacingStores(t)
	harness := &fakeHarness{}
	for round := range dispatchRaceRounds {
		session := sessions.ID(fmt.Sprintf("s%d", round))
		seedDispatchSession(t, first, session)
		command := startCommandFor(execution.IntentID("start-"+string(session)), session)
		var results [2]dispatch.Result
		var failures [2]error
		raceTogether(round,
			func() { results[0], failures[0] = run(dispatchService(first), harness, command) },
			func() { results[1], failures[1] = run(dispatchService(second), harness, command) },
		)
		if err := errors.Join(failures[:]...); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		kinds := []dispatch.ResultKind{results[0].Kind, results[1].Kind}
		if !slices.Contains(kinds, dispatch.ResultDispatched) || slices.Equal(kinds, []dispatch.ResultKind{dispatch.ResultDispatched, dispatch.ResultDispatched}) {
			t.Fatalf("round %d: results = %+v", round, results)
		}
		if harness.calls() != round+1 || countRows(t, first, "SELECT count(*) FROM dispatch_intents WHERE session_id = ?", string(session)) != 1 {
			t.Fatalf("round %d: calls %d", round, harness.calls())
		}
	}
}

// Two corrections of one revision with different IDs: exactly one is sent
// and advances; the other is refused (blocked, or stale once the first
// advanced).
func TestTwoStoresSteerOneRevisionOnce(t *testing.T) {
	first, second := openRacingStores(t)
	for round := range dispatchRaceRounds {
		session := sessions.ID(fmt.Sprintf("s%d", round))
		seedDispatchSession(t, first, session)
		turn := startAccepted(t, first, session)
		harness := &fakeHarness{}
		var results [2]dispatch.Result
		var failures [2]error
		raceTogether(round,
			func() {
				results[0], failures[0] = run(dispatchService(first), harness, steerCommandFor(execution.IntentID("a-"+string(session)), session, turn))
			},
			func() {
				results[1], failures[1] = run(dispatchService(second), harness, steerCommandFor(execution.IntentID("b-"+string(session)), session, turn))
			},
		)
		if err := errors.Join(failures[:]...); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		dispatched, refused := 0, 0
		for _, result := range results {
			switch {
			case result.Kind == dispatch.ResultDispatched && result.Outcome.Classification == execution.ClassAdvanced:
				dispatched++
			case result.Kind == dispatch.ResultIntentRefused &&
				(result.Intent.Kind == execution.IntentBlockedByOpenIntent || result.Intent.Kind == execution.IntentStaleRevision):
				refused++
			}
		}
		if dispatched != 1 || refused != 1 || harness.calls() != 1 || sessionRevision(t, first, session) != 4 {
			t.Fatalf("round %d: results = %+v, calls %d", round, results, harness.calls())
		}
	}
}

// Two stores claiming one recorded intent: exactly one claims and sends.
func TestTwoStoresClaimOneIntentOnce(t *testing.T) {
	first, second := openRacingStores(t)
	harness := &fakeHarness{}
	for round := range dispatchRaceRounds {
		session := sessions.ID(fmt.Sprintf("s%d", round))
		seedDispatchSession(t, first, session)
		command := startCommandFor(execution.IntentID("start-"+string(session)), session)
		crashed := dispatch.Service{Store: crashingStore{Store: first.Dispatch(), claim: true}, Now: func() time.Time { return dispatchNow }}
		if _, err := run(crashed, harness, command); !errors.Is(err, errCrash) {
			t.Fatal(err)
		}
		var results [2]dispatch.Result
		var failures [2]error
		raceTogether(round,
			func() { results[0], failures[0] = run(dispatchService(first), harness, command) },
			func() { results[1], failures[1] = run(dispatchService(second), harness, command) },
		)
		if err := errors.Join(failures[:]...); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		kinds := []dispatch.ResultKind{results[0].Kind, results[1].Kind}
		slices.Sort(kinds)
		if kinds[0] != dispatch.ResultDeliveryUnknown && kinds[0] != dispatch.ResultDispatched || !slices.Contains(kinds, dispatch.ResultDispatched) ||
			slices.Equal(kinds, []dispatch.ResultKind{dispatch.ResultDispatched, dispatch.ResultDispatched}) || harness.calls() != round+1 {
			t.Fatalf("round %d: results = %+v, calls %d", round, results, harness.calls())
		}
	}
}

// A unit of work changing the binding races the claim: while the intent is
// open the database refuses the binding change, so the claim always sends
// with the binding it recorded. A save that runs only after the outcome
// closed the intent may change the binding. The unit of work's deferred
// transaction may also lose to a commit as a stale snapshot.
func TestUnitOfWorkSaveRacesClaim(t *testing.T) {
	first, second := openRacingStores(t)
	ctx := context.Background()
	for round := range dispatchRaceRounds {
		session := sessions.ID(fmt.Sprintf("s%d", round))
		seedDispatchSession(t, first, session)
		command := startCommandFor(execution.IntentID("start-"+string(session)), session)
		crashed := dispatch.Service{Store: crashingStore{Store: first.Dispatch(), claim: true}, Now: func() time.Time { return dispatchNow }}
		if _, err := run(crashed, &fakeHarness{}, command); !errors.Is(err, errCrash) {
			t.Fatal(err)
		}
		harness := &fakeHarness{}
		var result dispatch.Result
		var claimErr, saveErr error
		raceTogether(round,
			func() { result, claimErr = run(dispatchService(first), harness, command) },
			func() {
				saveErr = saveBinding(ctx, second, session, func(r *sessions.Record) { r.AssignmentID = "other" })
			},
		)
		if claimErr != nil || result.Kind != dispatch.ResultDispatched || harness.starts[0].Assignment.AssignmentID != "assignment" ||
			saveErr != nil && !errors.Is(saveErr, sessions.ErrBindingLocked) && !staleSnapshot(saveErr) {
			t.Fatalf("round %d: claim %+v, %v; save %v", round, result, claimErr, saveErr)
		}
		if saveErr == nil && intentState(t, first, command.IntentID) != "accepted" {
			t.Fatalf("round %d: binding changed while the intent was open", round)
		}
	}
}

// A unit of work changing the binding races a Steer's outcome: either the
// save runs first and is refused while the intent is open, or the outcome
// moves the revision first and the save fails its version check. Never both.
func TestUnitOfWorkSaveRacesOutcome(t *testing.T) {
	first, second := openRacingStores(t)
	ctx := context.Background()
	wins := map[string]int{}
	for round := range dispatchRaceRounds {
		session := sessions.ID(fmt.Sprintf("s%d", round))
		seedDispatchSession(t, first, session)
		turn := startAccepted(t, first, session)
		stale, err := second.Sessions().Get(ctx, session)
		if err != nil {
			t.Fatal(err)
		}
		release, reached, settled := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		releaseHarness := func() { releaseOnce.Do(func() { close(release) }) }
		// Cleanup releases a fake still blocked after a failed round.
		t.Cleanup(releaseHarness)
		harness := &fakeHarness{onSteer: func(_ context.Context, r provider.SteerRequest) (provider.DeliveryReceipt, error) {
			close(reached)
			<-release
			return codexReceipt(r), nil
		}}
		var result dispatch.Result
		var steerErr, saveErr error
		go func() {
			defer close(settled)
			result, steerErr = run(dispatchService(first), harness, steerCommandFor(execution.IntentID("steer-"+string(session)), session, turn))
		}()
		select {
		case <-reached:
		case <-settled:
			t.Fatalf("round %d: steer ended before the harness: %+v, %v", round, result, steerErr)
		case <-time.After(10 * time.Second):
			releaseHarness()
			t.Fatalf("round %d: harness not reached within 10s", round)
		}
		raceTogether(round,
			func() { releaseHarness(); <-settled },
			func() {
				changed := stale
				changed.AssignmentID = "other"
				saveErr = second.WithinUnitOfWork(ctx, func(writer sessions.Writer, _ events.Appender) error {
					_, err := writer.Save(ctx, changed, stale.Version)
					return err
				})
			},
		)
		switch {
		case errors.Is(saveErr, sessions.ErrBindingLocked):
			wins["refused while open"]++
		case errors.Is(saveErr, sessions.ErrVersionConflict), staleSnapshot(saveErr):
			wins["outcome first"]++
		default:
			t.Fatalf("round %d: save = %v", round, saveErr)
		}
		got, err := first.Sessions().Get(ctx, session)
		if steerErr != nil || result.Outcome.Classification != execution.ClassAdvanced || err != nil || got.Revision != 4 || got.AssignmentID != "assignment" {
			t.Fatalf("round %d: steer %+v, %v; session %+v, %v", round, result, steerErr, got, err)
		}
	}
	t.Logf("wins = %v", wins)
}

// saveBinding reads the session, then saves the change in a unit of work;
// the pool's one connection is the unit of work's while it runs.
func saveBinding(ctx context.Context, store *Store, id sessions.ID, change func(*sessions.Record)) error {
	record, err := store.Sessions().Get(ctx, id)
	if err != nil {
		return err
	}
	expected := record.Version
	change(&record)
	return store.WithinUnitOfWork(ctx, func(writer sessions.Writer, _ events.Appender) error {
		_, err := writer.Save(ctx, record, expected)
		return err
	})
}

// staleSnapshot is Turso's refusal of a deferred transaction's write after
// another connection committed; nothing was written.
func staleSnapshot(err error) bool {
	return err != nil && strings.Contains(err.Error(), "snapshot is stale")
}
