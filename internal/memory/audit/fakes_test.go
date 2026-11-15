package audit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

var auditTime = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

var errInjected = errors.New("injected failure")

// fakeStore keeps records, versions, suggestions and deletion facts in
// memory and appends one line per write. Transaction number failTransaction
// (from 1) fails before it runs; failures inject an error by method name.
type fakeStore struct {
	records     map[string]records.RecordFacts
	versions    map[string]records.VersionFacts
	suggestions map[string]StoredSuggestion
	// dismissed holds "version|hash|action" of the human dismissals.
	dismissed     map[string]bool
	deletionFacts map[string]deletion.RecordFacts
	failures      map[string]error

	failTransaction  int
	afterTransaction func()
	transactions     int
	reads            []string
	writes           []string
	reviews          []records.Review
	changes          []records.StatusChange
	deletions        []deletion.Deletion
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		records:       make(map[string]records.RecordFacts),
		versions:      make(map[string]records.VersionFacts),
		suggestions:   make(map[string]StoredSuggestion),
		dismissed:     make(map[string]bool),
		deletionFacts: make(map[string]deletion.RecordFacts),
		failures:      make(map[string]error),
	}
}

func (s *fakeStore) within(operation func(fakeTx) error) error {
	s.transactions++
	if s.transactions == s.failTransaction {
		return errInjected
	}
	err := operation(fakeTx{s})
	if s.afterTransaction != nil {
		s.afterTransaction()
	}
	return err
}

func (s *fakeStore) WithinRecordWrite(_ context.Context, operation func(records.Transaction) error) error {
	return s.within(func(tx fakeTx) error { return operation(tx) })
}

func (s *fakeStore) WithinSuggestionConfirmation(_ context.Context, operation func(ConfirmationTransaction) error) error {
	return s.within(func(tx fakeTx) error { return operation(tx) })
}

type fakeTx struct{ store *fakeStore }

func (t fakeTx) fail(method string) error { return t.store.failures[method] }

func (t fakeTx) ReadRecord(_ context.Context, recordID string) (records.RecordFacts, error) {
	t.store.reads = append(t.store.reads, "record "+recordID)
	return t.store.records[recordID], t.fail("ReadRecord")
}

func (t fakeTx) ReadVersion(_ context.Context, recordID, versionID string) (records.VersionFacts, error) {
	t.store.reads = append(t.store.reads, "version "+recordID+"/"+versionID)
	return t.store.versions[recordID+"/"+versionID], t.fail("ReadVersion")
}

func (t fakeTx) ReadSuggestionDismissed(_ context.Context, versionID, contentHash string, suggestion memory.Suggestion) (bool, error) {
	key := versionID + "|" + contentHash + "|" + string(suggestion)
	t.store.reads = append(t.store.reads, "dismissal "+key)
	return t.store.dismissed[key], t.fail("ReadSuggestionDismissed")
}

func (fakeTx) ReadExpiredProposals(context.Context, time.Time, records.ProposalPosition, int) ([]records.ProposalPosition, error) {
	return nil, nil
}

func (fakeTx) SearchTextBytes(content string) int64 { return int64(len(strings.ToLower(content))) }

func (fakeTx) InsertRecord(context.Context, records.NewRecord) error { return nil }

func (t fakeTx) ChangeStatus(_ context.Context, change records.StatusChange) error {
	t.store.writes = append(t.store.writes, fmt.Sprintf("status %s %s->%s", change.RecordID, change.From, change.To))
	t.store.changes = append(t.store.changes, change)
	return t.fail("ChangeStatus")
}

func (fakeTx) CopyVersion(context.Context, records.VersionCopy) error { return nil }

func (t fakeTx) InsertReview(_ context.Context, review records.Review) error {
	line := fmt.Sprintf("review %s %s by %s", review.VersionID, review.Decision, review.Actor)
	if review.Suggestion != "" {
		line += " suggests " + string(review.Suggestion)
	}
	t.store.writes = append(t.store.writes, line)
	t.store.reviews = append(t.store.reviews, review)
	return t.fail("InsertReview")
}

func (fakeTx) InsertRelation(context.Context, records.Relation) error { return nil }

func (t fakeTx) ReadRecordDeletionFacts(_ context.Context, recordID string) (deletion.RecordFacts, error) {
	t.store.reads = append(t.store.reads, "deletion "+recordID)
	return t.store.deletionFacts[recordID], t.fail("ReadRecordDeletionFacts")
}

func (t fakeTx) DeleteRecordContent(_ context.Context, d deletion.Deletion) error {
	t.store.writes = append(t.store.writes, fmt.Sprintf("delete %s %d versions", d.RecordID, len(d.Versions)))
	t.store.deletions = append(t.store.deletions, d)
	return t.fail("DeleteRecordContent")
}

func (t fakeTx) ReadSuggestion(_ context.Context, reviewID string) (StoredSuggestion, error) {
	t.store.reads = append(t.store.reads, "suggestion "+reviewID)
	return t.store.suggestions[reviewID], t.fail("ReadSuggestion")
}

func sequentialIDs() func(string) (string, error) {
	next := 0
	return func(prefix string) (string, error) {
		next++
		return fmt.Sprintf("%s-%d", prefix, next), nil
	}
}

func auditService(store *fakeStore) Service {
	return Service{
		Records:       records.Service{Store: store, NewID: sequentialIDs()},
		Deletion:      deletion.Service{NewID: sequentialIDs()},
		Confirmations: store,
		NewID:         sequentialIDs(),
	}
}

func wantWrites(t *testing.T, store *fakeStore, want ...string) {
	t.Helper()
	if strings.Join(store.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%s\nwant:\n%s", strings.Join(store.writes, "\n"), strings.Join(want, "\n"))
	}
}

// storedRecord is record id of project p1 at version 3 in status, whose
// current version is id+"-v" with content hash hashOf(id), as entry names it.
func storedRecord(id string, status memory.RecordStatus) records.RecordFacts {
	return records.RecordFacts{
		Found:              true,
		Record:             memory.Record{ID: id, ProjectID: "p1", Category: memory.CategoryFact, Status: status, CurrentVersionID: id + "-v", Version: 3, CreatedAt: auditTime.Add(-time.Hour)},
		CurrentContentHash: hashOf(id),
	}
}

// addRecord stores the record and its current version for both the record
// write and the deletion reads.
func (s *fakeStore) addRecord(facts records.RecordFacts) {
	s.records[facts.Record.ID] = facts
	s.versions[facts.Record.ID+"/"+facts.Record.CurrentVersionID] = records.VersionFacts{Found: true, ContentHash: facts.CurrentContentHash, ContentDeleted: facts.CurrentContentDeleted}
	s.deletionFacts[facts.Record.ID] = deletion.RecordFacts{
		Found:    true,
		Record:   facts.Record,
		Versions: []memory.VersionDeletionState{{VersionID: facts.Record.CurrentVersionID, ContentHash: facts.CurrentContentHash, ContentDeleted: facts.CurrentContentDeleted}},
	}
}

// storedSuggestion is the agent's suggestion review-<id> about the record's
// current version.
func storedSuggestion(record records.RecordFacts, action memory.Suggestion) StoredSuggestion {
	return StoredSuggestion{
		ReviewID:   "review-" + record.Record.ID,
		Reviewer:   "auditor",
		Reason:     "obsolete",
		ReviewedAt: auditTime,
		Standing: memory.SuggestionStandingFacts{
			ReviewFound: true, Actor: memory.ActorAgent, Decision: memory.ReviewOpinion, Suggestion: action,
			VersionID: record.Record.CurrentVersionID, ContentHash: record.CurrentContentHash,
			RecordFound: true, Record: record.Record, CurrentContentHash: record.CurrentContentHash, CurrentContentDeleted: record.CurrentContentDeleted,
		},
	}
}
