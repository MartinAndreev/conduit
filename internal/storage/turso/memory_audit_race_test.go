package turso

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/audit"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

// suggestionRaceRounds is how many times each race runs, each on a new record
// of the same database.
const suggestionRaceRounds = 20

// raceStaggerSteps and raceStaggerStep bound the delay raceTogether gives
// one of the racing operations.
const (
	raceStaggerSteps = 5
	raceStaggerStep  = 250 * time.Microsecond
)

// openRacingStores opens two stores on one file. Each has its own
// connection, so racing writers meet at the database write lock that
// withMemoryWrite takes with BEGIN IMMEDIATE, not at a shared pool.
func openRacingStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	var stores [2]*Store
	for index := range stores {
		store, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		stores[index] = store
	}
	return stores[0], stores[1]
}

// raceTogether runs both operations in their own goroutines, released
// together from a start barrier once both are waiting, and returns when both
// finished. The operations do different work before their transactions (a
// recording extracts the report, a dismissal redacts its reason), so one
// would nearly always reach the write lock first. Each round therefore
// delays one of them after the barrier, alternating which, by 0 to
// raceStaggerSteps-1 steps, so that both orders occur.
func raceTogether(round int, first, second func()) {
	operations := []func(){first, second}
	delayed := round % 2
	stagger := time.Duration(round/2%raceStaggerSteps) * raceStaggerStep
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	for index, operation := range operations {
		ready.Add(1)
		done.Go(func() {
			ready.Done()
			<-start
			if index == delayed {
				time.Sleep(stagger)
			}
			operation()
		})
	}
	ready.Wait()
	close(start)
	done.Wait()
}

// suggestedRecord creates a new human record of round and records the
// agent's suggestion of action for its first version.
func suggestedRecord(t *testing.T, store *Store, round int, action memory.Suggestion) (records.CreateResult, string) {
	t.Helper()
	created := humanAt(t, store, fmt.Sprintf("Запис за състезание %s %d", action, round), round)
	recorded := mustRecordSuggestions(t, store, auditReport(suggestionEntry(created, action, "предложение")), recordsNow.Add(10*time.Minute))
	if len(recorded.Recorded) != 1 || recorded.Recorded[0].Result.Kind != memory.OpinionAdd {
		t.Fatalf("recorded = %+v", recorded)
	}
	return created, recorded.Recorded[0].Result.ReviewID
}

func dismissRequest(reviewID string) audit.DismissRequest {
	return audit.DismissRequest{
		ProjectID: "project", ReviewID: reviewID, ExpectedVersion: 1,
		Actor: memory.ActorHuman, Reviewer: "martin", Reason: "още е нужно", Now: recordsNow.Add(2 * time.Hour),
	}
}

func confirmRequest(reviewID string) audit.ConfirmRequest {
	return audit.ConfirmRequest{
		ProjectID: "project", ReviewID: reviewID, ExpectedVersion: 1,
		Actor: memory.ActorHuman, Reviewer: "martin", Reason: "съгласен", Now: recordsNow.Add(time.Hour),
	}
}

// countOf runs a count query with one argument.
func countOf(t *testing.T, store *Store, query string, arg string) int {
	t.Helper()
	got := queryStrings(t, store, "SELECT CAST(count(*) AS TEXT) FROM "+query, arg)
	count, err := strconv.Atoi(got[0])
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func dismissalsOf(t *testing.T, store *Store, versionID string) int {
	t.Helper()
	return countOf(t, store, "memory_reviews WHERE decision = 'dismiss' AND version_id = ?", versionID)
}

// confirmedEffects counts the writes a confirmation of action makes for the
// record: the human's archive opinion, or the version's deletion receipt.
func confirmedEffects(t *testing.T, store *Store, action memory.Suggestion, created records.CreateResult) int {
	t.Helper()
	if action == memory.SuggestArchive {
		return countOf(t, store, "memory_reviews WHERE actor = 'human' AND decision = 'opinion' AND version_id = ?", created.VersionID)
	}
	return countOf(t, store, "context_deletion_receipts WHERE subject_id = ?", created.VersionID)
}

// recordState is the record's status and version, as "status:version".
func recordState(t *testing.T, store *Store, recordID string) string {
	t.Helper()
	return queryStrings(t, store, "SELECT status || ':' || CAST(version AS TEXT) FROM memory_records WHERE id = ?", recordID)[0]
}

// confirmedState is the record's state after one confirmation of action.
func confirmedState(action memory.Suggestion) string {
	if action == memory.SuggestArchive {
		return "archived:2"
	}
	return "active:2"
}

// confirmedRefusal is what a confirmation or a dismissal reads after the
// other confirmed action: an archive moved the status; a delete removed the
// content.
func confirmedRefusal(action memory.Suggestion) (memory.SuggestionKind, memory.SuggestionDismissalKind) {
	if action == memory.SuggestArchive {
		return memory.SuggestionStatusMoved, memory.SuggestionDismissalStatusMoved
	}
	return memory.SuggestionContentDeleted, memory.SuggestionDismissalContentDeleted
}

// pendingFor lists the record's pending suggestions as "action:version".
func pendingFor(t *testing.T, store *Store, recordID string) []string {
	t.Helper()
	var listed []string
	after := audit.SuggestionPosition{}
	for {
		page, err := auditService(store).PendingSuggestions(context.Background(), audit.PendingRequest{ProjectID: "project", After: after, Limit: audit.MaxPendingPageSize})
		if err != nil {
			t.Fatal(err)
		}
		for _, suggestion := range page.Suggestions {
			if suggestion.Record.ID == recordID {
				listed = append(listed, string(suggestion.Action)+":"+suggestion.VersionID)
			}
		}
		if page.Next == (audit.SuggestionPosition{}) {
			return listed
		}
		after = page.Next
	}
}

// A dismissal and a confirmation of one suggestion serialize on the write
// lock: exactly one wins. When the dismissal wins, the confirmation reads it
// and is refused as dismissed, and the record is unchanged; when the
// confirmation wins, its action is applied once and the dismissal is refused
// by the moved status (archive) or the deleted content (delete), with no
// dismissal row. Never both a dismissal and the action.
func TestDismissAndConfirmOfOneSuggestionRace(t *testing.T) {
	for _, action := range memory.Suggestions() {
		t.Run(string(action), func(t *testing.T) {
			first, second := openRacingStores(t)
			_, dismissRefused := confirmedRefusal(action)
			wins := map[string]int{}
			for round := range suggestionRaceRounds {
				created, reviewID := suggestedRecord(t, first, round, action)
				var dismissed audit.DismissResult
				var confirmed audit.ConfirmResult
				var dismissErr, confirmErr error
				raceTogether(round,
					func() {
						dismissed, dismissErr = auditService(first).DismissSuggestion(context.Background(), dismissRequest(reviewID))
					},
					func() {
						confirmed, confirmErr = auditService(second).ConfirmSuggestion(context.Background(), confirmRequest(reviewID))
					},
				)
				if err := errors.Join(dismissErr, confirmErr); err != nil {
					t.Fatalf("round %d: %v", round, err)
				}
				dismissals, effects := dismissalsOf(t, first, created.VersionID), confirmedEffects(t, first, action, created)
				state := recordState(t, first, created.RecordID)
				switch {
				case dismissed.Kind == memory.SuggestionDismiss:
					wins["dismiss"]++
					if dismissed.Action != action || confirmed.Kind != memory.SuggestionDismissed || confirmed.Applied ||
						dismissals != 1 || effects != 0 || state != "active:1" {
						t.Fatalf("round %d, dismissal won: dismiss %+v, confirm %+v, dismissals %d, effects %d, record %s", round, dismissed, confirmed, dismissals, effects, state)
					}
				case confirmed.Kind == memory.SuggestionApplies && confirmed.Applied:
					wins["confirm"]++
					if confirmed.Action != action || dismissed.Kind != dismissRefused || dismissed.DismissalID != "" ||
						dismissals != 0 || effects != 1 || state != confirmedState(action) {
						t.Fatalf("round %d, confirmation won: dismiss %+v, confirm %+v, dismissals %d, effects %d, record %s", round, dismissed, confirmed, dismissals, effects, state)
					}
				default:
					t.Fatalf("round %d, neither won: dismiss %+v, confirm %+v", round, dismissed, confirmed)
				}
				if pending := pendingFor(t, second, created.RecordID); len(pending) != 0 {
					t.Fatalf("round %d: pending = %v", round, pending)
				}
			}
			t.Logf("wins = %v", wins)
		})
	}
}

// Two dismissals of one suggestion serialize on the write lock: one writes
// the dismissal, the other reads it and is refused as already dismissed.
// There is never a second dismissal row.
func TestDismissalsOfOneSuggestionRace(t *testing.T) {
	first, second := openRacingStores(t)
	for round := range suggestionRaceRounds {
		created, reviewID := suggestedRecord(t, first, round, memory.SuggestArchive)
		var results [2]audit.DismissResult
		var failures [2]error
		raceTogether(round,
			func() {
				results[0], failures[0] = auditService(first).DismissSuggestion(context.Background(), dismissRequest(reviewID))
			},
			func() {
				results[1], failures[1] = auditService(second).DismissSuggestion(context.Background(), dismissRequest(reviewID))
			},
		)
		if err := errors.Join(failures[:]...); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		kinds := []memory.SuggestionDismissalKind{results[0].Kind, results[1].Kind}
		slices.Sort(kinds)
		if !slices.Equal(kinds, []memory.SuggestionDismissalKind{memory.SuggestionAlreadyDismissed, memory.SuggestionDismiss}) {
			t.Fatalf("round %d: results = %+v", round, results)
		}
		winner := results[0]
		if winner.Kind != memory.SuggestionDismiss {
			winner = results[1]
		}
		ids := queryStrings(t, first, "SELECT id FROM memory_reviews WHERE decision = 'dismiss' AND version_id = ?", created.VersionID)
		if !slices.Equal(ids, []string{winner.DismissalID}) {
			t.Fatalf("round %d: dismissals = %v, winner %+v", round, ids, winner)
		}
		if state := recordState(t, first, created.RecordID); state != "active:1" {
			t.Fatalf("round %d: record = %s", round, state)
		}
		if pending := pendingFor(t, second, created.RecordID); len(pending) != 0 {
			t.Fatalf("round %d: pending = %v", round, pending)
		}
	}
}

// A dismissal and the agent's recording of the same action for the same
// version serialize on the write lock. Either the agent's opinion is
// recorded first and the dismissal, bound to the version, content hash and
// action, then covers it too; or the dismissal is first and the recording is
// refused as suggestion_dismissed, writing nothing. The dismissal always
// wins, and no suggestion of the dismissed action stays pending for that
// version.
func TestDismissalAndAgentRecordingOfOneActionRace(t *testing.T) {
	first, second := openRacingStores(t)
	order := map[string]int{}
	for round := range suggestionRaceRounds {
		created, reviewID := suggestedRecord(t, first, round, memory.SuggestArchive)
		var dismissed audit.DismissResult
		var recorded audit.RecordResult
		var dismissErr, recordErr error
		request := audit.RecordRequest{
			Report:    auditReport(suggestionEntry(created, memory.SuggestArchive, "пак остаряло")),
			ProjectID: "project", Reviewer: "auditor", Now: recordsNow.Add(3 * time.Hour),
		}
		raceTogether(round,
			func() {
				dismissed, dismissErr = auditService(first).DismissSuggestion(context.Background(), dismissRequest(reviewID))
			},
			func() {
				recorded, recordErr = auditService(second).RecordSuggestions(context.Background(), request)
			},
		)
		if err := errors.Join(dismissErr, recordErr); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if dismissed.Kind != memory.SuggestionDismiss || len(recorded.Recorded) != 1 {
			t.Fatalf("round %d: dismiss %+v, recorded %+v", round, dismissed, recorded)
		}
		opinion := recorded.Recorded[0].Result
		suggestions := countOf(t, first, "memory_reviews WHERE actor = 'agent' AND suggestion = 'archive' AND version_id = ?", created.VersionID)
		switch opinion.Kind {
		case memory.OpinionAdd:
			order["recorded first"]++
			if opinion.ReviewID == "" || suggestions != 2 {
				t.Fatalf("round %d, recorded first: opinion %+v, agent suggestions %d", round, opinion, suggestions)
			}
		case memory.OpinionSuggestionDismissed:
			order["dismissed first"]++
			if opinion.ReviewID != "" || suggestions != 1 {
				t.Fatalf("round %d, dismissed first: opinion %+v, agent suggestions %d", round, opinion, suggestions)
			}
		default:
			t.Fatalf("round %d: opinion %+v", round, opinion)
		}
		if dismissals := dismissalsOf(t, first, created.VersionID); dismissals != 1 {
			t.Fatalf("round %d: dismissals = %d", round, dismissals)
		}
		if pending := pendingFor(t, second, created.RecordID); len(pending) != 0 {
			t.Fatalf("round %d: pending = %v", round, pending)
		}
	}
	t.Logf("order = %v", order)
}

// Two confirmations of one suggestion serialize on the write lock: one
// applies the action, the other reads its effect and is refused by the moved
// status (archive) or the deleted content (delete). The action is written
// once.
func TestConfirmationsOfOneSuggestionRace(t *testing.T) {
	for _, action := range memory.Suggestions() {
		t.Run(string(action), func(t *testing.T) {
			first, second := openRacingStores(t)
			refused, _ := confirmedRefusal(action)
			for round := range suggestionRaceRounds {
				created, reviewID := suggestedRecord(t, first, round, action)
				var results [2]audit.ConfirmResult
				var failures [2]error
				raceTogether(round,
					func() {
						results[0], failures[0] = auditService(first).ConfirmSuggestion(context.Background(), confirmRequest(reviewID))
					},
					func() {
						results[1], failures[1] = auditService(second).ConfirmSuggestion(context.Background(), confirmRequest(reviewID))
					},
				)
				if err := errors.Join(failures[:]...); err != nil {
					t.Fatalf("round %d: %v", round, err)
				}
				winner, loser := results[0], results[1]
				if !winner.Applied {
					winner, loser = loser, winner
				}
				if winner.Kind != memory.SuggestionApplies || !winner.Applied || winner.Action != action || loser.Kind != refused || loser.Applied {
					t.Fatalf("round %d: results = %+v", round, results)
				}
				if effects := confirmedEffects(t, first, action, created); effects != 1 {
					t.Fatalf("round %d: effects = %d", round, effects)
				}
				if state := recordState(t, first, created.RecordID); state != confirmedState(action) {
					t.Fatalf("round %d: record = %s", round, state)
				}
				if dismissals := dismissalsOf(t, first, created.VersionID); dismissals != 0 {
					t.Fatalf("round %d: dismissals = %d", round, dismissals)
				}
				if pending := pendingFor(t, second, created.RecordID); len(pending) != 0 {
					t.Fatalf("round %d: pending = %v", round, pending)
				}
			}
		})
	}
}
