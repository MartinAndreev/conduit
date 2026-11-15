package records

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
)

var writeTime = time.Date(2026, 11, 12, 10, 0, 0, 5, time.FixedZone("EET", 2*60*60))

// fakeStore records what a procedure did inside its one transaction. Each
// write is appended as one line to writes; failures are injected by method
// name.
type fakeStore struct {
	records  map[string]RecordFacts
	versions map[string]VersionFacts
	// dismissed holds "version|hash|action" of the human dismissals.
	dismissed map[string]bool
	expired   []ProposalPosition
	failures  map[string]error
	withinErr error

	transactions int
	reads        []string
	writes       []string
	created      []NewRecord
	changes      []StatusChange
	copies       []VersionCopy
	reviews      []Review
	relations    []Relation
	cutoff       time.Time
	after        ProposalPosition
	limit        int
}

func newFakeStore(records ...RecordFacts) *fakeStore {
	store := &fakeStore{records: make(map[string]RecordFacts), versions: make(map[string]VersionFacts), dismissed: make(map[string]bool), failures: make(map[string]error)}
	for _, record := range records {
		store.records[record.Record.ID] = record
	}
	return store
}

func (s *fakeStore) WithinRecordWrite(_ context.Context, operation func(Transaction) error) error {
	s.transactions++
	if s.withinErr != nil {
		return s.withinErr
	}
	return operation(fakeTransaction{s})
}

type fakeTransaction struct{ store *fakeStore }

func (t fakeTransaction) fail(method string) error { return t.store.failures[method] }

func (t fakeTransaction) ReadRecord(_ context.Context, recordID string) (RecordFacts, error) {
	t.store.reads = append(t.store.reads, "record "+recordID)
	return t.store.records[recordID], t.fail("ReadRecord")
}

func (t fakeTransaction) ReadVersion(_ context.Context, recordID, versionID string) (VersionFacts, error) {
	t.store.reads = append(t.store.reads, "version "+recordID+"/"+versionID)
	return t.store.versions[recordID+"/"+versionID], t.fail("ReadVersion")
}

func (t fakeTransaction) ReadSuggestionDismissed(_ context.Context, versionID, contentHash string, suggestion memory.Suggestion) (bool, error) {
	key := versionID + "|" + contentHash + "|" + string(suggestion)
	t.store.reads = append(t.store.reads, "dismissal "+key)
	return t.store.dismissed[key], t.fail("ReadSuggestionDismissed")
}

// ReadExpiredProposals pages store.expired, which tests keep in sweep order,
// as the real store does.
func (t fakeTransaction) ReadExpiredProposals(_ context.Context, cutoff time.Time, after ProposalPosition, limit int) ([]ProposalPosition, error) {
	t.store.reads = append(t.store.reads, "expired proposals")
	t.store.cutoff, t.store.after, t.store.limit = cutoff, after, limit
	start := 0
	if after.RecordID != "" {
		start = slices.Index(t.store.expired, after) + 1
	}
	end := min(start+limit, len(t.store.expired))
	return t.store.expired[start:end], t.fail("ReadExpiredProposals")
}

func (t fakeTransaction) SearchTextBytes(content string) int64 {
	return int64(len(strings.ToLower(content)))
}

func (t fakeTransaction) InsertRecord(_ context.Context, record NewRecord) error {
	t.store.writes = append(t.store.writes, "insert record "+record.Record.ID)
	t.store.created = append(t.store.created, record)
	return t.fail("InsertRecord")
}

func (t fakeTransaction) ChangeStatus(_ context.Context, change StatusChange) error {
	t.store.writes = append(t.store.writes, fmt.Sprintf("status %s %s->%s", change.RecordID, change.From, change.To))
	t.store.changes = append(t.store.changes, change)
	return t.fail("ChangeStatus")
}

func (t fakeTransaction) CopyVersion(_ context.Context, version VersionCopy) error {
	t.store.writes = append(t.store.writes, "copy "+version.FromVersionID+"->"+version.VersionID)
	t.store.copies = append(t.store.copies, version)
	return t.fail("CopyVersion")
}

func (t fakeTransaction) InsertReview(_ context.Context, review Review) error {
	t.store.writes = append(t.store.writes, fmt.Sprintf("review %s %s by %s", review.VersionID, review.Decision, review.Actor))
	t.store.reviews = append(t.store.reviews, review)
	return t.fail("InsertReview")
}

func (t fakeTransaction) InsertRelation(_ context.Context, relation Relation) error {
	t.store.writes = append(t.store.writes, fmt.Sprintf("relation %s %s %s", relation.FromRecordID, relation.Kind, relation.ToRecordID))
	t.store.relations = append(t.store.relations, relation)
	return t.fail("InsertRelation")
}

// fakeSnapshot maps paths to observations; err fails every observation.
// observed lists every observed path in order, batches the ObserveFiles calls.
type fakeSnapshot struct {
	files    map[string]compose.FileObservation
	err      error
	observed []string
	batches  int
}

func (f *fakeSnapshot) ObserveFiles(_ context.Context, _ compose.SnapshotRef, paths []string) ([]compose.FileObservation, error) {
	f.batches++
	f.observed = append(f.observed, paths...)
	if f.err != nil {
		return nil, f.err
	}
	observed := make([]compose.FileObservation, 0, len(paths))
	for _, path := range paths {
		if observation, ok := f.files[path]; ok {
			observed = append(observed, observation)
			continue
		}
		observed = append(observed, compose.FileObservation{Observation: memory.SourceMissing})
	}
	return observed, nil
}

func sequentialIDs() func(string) (string, error) {
	next := 0
	return func(prefix string) (string, error) {
		next++
		return fmt.Sprintf("%s-%d", prefix, next), nil
	}
}

// failingIDs fails the call numbered failAt (from 1).
func failingIDs(failAt int, err error) func(string) (string, error) {
	next := 0
	return func(prefix string) (string, error) {
		next++
		if next == failAt {
			return "", err
		}
		return fmt.Sprintf("%s-%d", prefix, next), nil
	}
}

const currentHash = "sha256:current"

// storedRecord is record r1 of project p1 at version 3 in status, with its
// current version v1 holding two file sources and a spec source.
func storedRecord(status memory.RecordStatus) RecordFacts {
	return RecordFacts{
		Found:              true,
		Record:             memory.Record{ID: "r1", ProjectID: "p1", Category: memory.CategoryFact, Status: status, CurrentVersionID: "v1", Version: 3, CreatedAt: writeTime.Add(-100 * 24 * time.Hour)},
		CurrentContentHash: currentHash,
		Sources: []memory.RecordSource{
			{Kind: memory.SourceFile, Path: "a.go", BlobHash: "388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"},
			{Kind: memory.SourceFile, Path: "b.go", BlobHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"},
			{Kind: memory.SourceSpec, Ref: "specs/012"},
		},
	}
}

func service(store *fakeStore) Service {
	return Service{Store: store, NewID: sequentialIDs()}
}

func wantNoWrites(t *testing.T, store *fakeStore) {
	t.Helper()
	if len(store.writes) != 0 {
		t.Fatalf("writes = %v", store.writes)
	}
}

func wantWrites(t *testing.T, store *fakeStore, want ...string) {
	t.Helper()
	if strings.Join(store.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%s\nwant:\n%s", strings.Join(store.writes, "\n"), strings.Join(want, "\n"))
	}
}

var errInjected = errors.New("injected failure")
