package proposals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

var reviewTime = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)

var errInjected = errors.New("injected failure")

// fakeRecords keeps records in memory and appends one line per write.
// Transaction number failTransaction (from 1) fails before it runs; a
// failing transaction writes nothing.
type fakeRecords struct {
	records         map[string]records.RecordFacts
	failTransaction int
	// afterTransaction runs after each transaction, for example to cancel.
	afterTransaction func()
	transactions     int
	writes           []string
	created          []records.NewRecord
	reviews          []records.Review
}

func newFakeRecords(stored ...records.RecordFacts) *fakeRecords {
	store := &fakeRecords{records: make(map[string]records.RecordFacts)}
	for _, record := range stored {
		store.records[record.Record.ID] = record
	}
	return store
}

func (s *fakeRecords) WithinRecordWrite(_ context.Context, operation func(records.Transaction) error) error {
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

type fakeTx struct{ store *fakeRecords }

func (t fakeTx) ReadRecord(_ context.Context, recordID string) (records.RecordFacts, error) {
	return t.store.records[recordID], nil
}

func (fakeTx) ReadVersion(context.Context, string, string) (records.VersionFacts, error) {
	return records.VersionFacts{}, nil
}

func (fakeTx) ReadExpiredProposals(context.Context, time.Time, records.ProposalPosition, int) ([]records.ProposalPosition, error) {
	return nil, nil
}

func (fakeTx) SearchTextBytes(content string) int64 { return int64(len(strings.ToLower(content))) }

func (t fakeTx) InsertRecord(_ context.Context, created records.NewRecord) error {
	scope := created.Version.Scope
	t.store.writes = append(t.store.writes, fmt.Sprintf("insert %s %s %s %s/%s", created.Record.ID, created.Record.Status, created.Version.Trust, scope.Visibility, scope.AttemptID))
	t.store.created = append(t.store.created, created)
	t.store.records[created.Record.ID] = records.RecordFacts{Found: true, Record: created.Record, CurrentContentHash: created.Version.ContentHash, Sources: created.Sources}
	return nil
}

func (t fakeTx) ChangeStatus(_ context.Context, change records.StatusChange) error {
	t.store.writes = append(t.store.writes, fmt.Sprintf("status %s %s->%s", change.RecordID, change.From, change.To))
	stored := t.store.records[change.RecordID]
	stored.Record.Status, stored.Record.Version = change.To, stored.Record.Version+1
	t.store.records[change.RecordID] = stored
	return nil
}

func (fakeTx) CopyVersion(context.Context, records.VersionCopy) error { return nil }

func (t fakeTx) InsertReview(_ context.Context, review records.Review) error {
	t.store.writes = append(t.store.writes, fmt.Sprintf("review %s %s", review.VersionID, review.Decision))
	t.store.reviews = append(t.store.reviews, review)
	return nil
}

func (fakeTx) InsertRelation(context.Context, records.Relation) error { return nil }

func sequentialIDs() func(string) (string, error) {
	next := 0
	return func(prefix string) (string, error) {
		next++
		return fmt.Sprintf("%s-%d", prefix, next), nil
	}
}

func proposalService(store *fakeRecords) Service {
	return Service{Records: records.Service{Store: store, NewID: sequentialIDs()}, Normalizer: lowerNormalizer{}}
}

func wantWrites(t *testing.T, store *fakeRecords, want ...string) {
	t.Helper()
	if strings.Join(store.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%s\nwant:\n%s", strings.Join(store.writes, "\n"), strings.Join(want, "\n"))
	}
}

// proposed is a proposal of project p1 at version 1 whose current version is
// the record ID with "-v".
func proposed(recordID string) records.RecordFacts {
	return records.RecordFacts{
		Found:              true,
		Record:             memory.Record{ID: recordID, ProjectID: "p1", Category: memory.CategoryFact, Status: memory.StatusProposed, CurrentVersionID: recordID + "-v", Version: 1, CreatedAt: reviewTime},
		CurrentContentHash: "sha256:" + recordID,
	}
}
