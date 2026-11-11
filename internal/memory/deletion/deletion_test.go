package deletion

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
)

var deletionTime = time.Date(2026, 11, 11, 10, 0, 0, 5, time.FixedZone("EET", 2*60*60))

// fakeStore records what the procedure did inside its one transaction.
type fakeStore struct {
	facts     RecordFacts
	readErr   error
	deleteErr error
	withinErr error

	transactions int
	readIDs      []string
	deletions    []Deletion
	callbackErr  error
}

func (s *fakeStore) WithinRecordDeletion(ctx context.Context, operation func(Transaction) error) error {
	s.transactions++
	if s.withinErr != nil {
		return s.withinErr
	}
	s.callbackErr = operation(fakeTransaction{s})
	return s.callbackErr
}

type fakeTransaction struct{ store *fakeStore }

func (t fakeTransaction) ReadRecordDeletionFacts(_ context.Context, recordID string) (RecordFacts, error) {
	t.store.readIDs = append(t.store.readIDs, recordID)
	return t.store.facts, t.store.readErr
}

func (t fakeTransaction) DeleteRecordContent(_ context.Context, deletion Deletion) error {
	t.store.deletions = append(t.store.deletions, deletion)
	return t.store.deleteErr
}

func storedRecord() RecordFacts {
	return RecordFacts{
		Found:  true,
		Record: memory.Record{ID: "record", ProjectID: "project", CurrentVersionID: "v2", Version: 4},
		Versions: []memory.VersionDeletionState{
			{VersionID: "v1", ContentHash: "h1", ContentDeleted: true},
			{VersionID: "v2", ContentHash: "h2"},
			{VersionID: "v3", ContentHash: "h3"},
		},
	}
}

func humanRequest() Request {
	return Request{ProjectID: "project", RecordID: "record", ExpectedVersion: 4, Actor: memory.ActorHuman, Now: deletionTime}
}

func sequentialIDs() func(string) (string, error) {
	next := 0
	return func(prefix string) (string, error) {
		next++
		return fmt.Sprintf("%s-%d", prefix, next), nil
	}
}

func TestDeleteRecordDeletesPendingVersionsWithReceipts(t *testing.T) {
	store := &fakeStore{facts: storedRecord()}
	got, err := Service{Store: store, NewID: sequentialIDs()}.DeleteRecord(context.Background(), humanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != memory.DeletionDelete || got.RecordVersion != 5 || len(got.Receipts) != 2 {
		t.Fatalf("result = %+v", got)
	}
	if store.transactions != 1 || !slices.Equal(store.readIDs, []string{"record"}) || len(store.deletions) != 1 {
		t.Fatalf("transactions %d, reads %v, deletions %d", store.transactions, store.readIDs, len(store.deletions))
	}
	deletion := store.deletions[0]
	utc := deletionTime.UTC()
	want := []memory.DeletionReceipt{
		{ID: "deletion-1", ProjectID: "project", SubjectKind: memory.DeletionSubjectMemoryVersion, SubjectID: "v2", ContentHash: "h2", DeletedAt: utc},
		{ID: "deletion-2", ProjectID: "project", SubjectKind: memory.DeletionSubjectMemoryVersion, SubjectID: "v3", ContentHash: "h3", DeletedAt: utc},
	}
	if !slices.Equal(deletion.Receipts, want) || !slices.Equal(got.Receipts, want) {
		t.Fatalf("receipts = %+v", deletion.Receipts)
	}
	if deletion.ProjectID != "project" || deletion.RecordID != "record" || deletion.ExpectedVersion != 4 || !deletion.DeletedAt.Equal(utc) || deletion.DeletedAt.Location() != time.UTC {
		t.Fatalf("deletion = %+v", deletion)
	}
	if !slices.Equal(deletion.Versions, storedRecord().Versions[1:]) {
		t.Fatalf("versions = %+v", deletion.Versions)
	}
}

func TestDeleteRecordWritesNothingForOtherDecisions(t *testing.T) {
	deleted := storedRecord()
	deleted.Versions[1].ContentDeleted, deleted.Versions[2].ContentDeleted = true, true
	foreign := storedRecord()
	foreign.Record.ProjectID = "other"
	for _, tc := range []struct {
		name   string
		facts  RecordFacts
		mutate func(*Request)
		want   memory.RecordDeletionKind
	}{
		{"already deleted", deleted, func(*Request) {}, memory.DeletionAlreadyDeleted},
		{"stale version", storedRecord(), func(r *Request) { r.ExpectedVersion = 3 }, memory.DeletionVersionConflict},
		{"agent", storedRecord(), func(r *Request) { r.Actor = memory.ActorAgent }, memory.DeletionActorNotAllowed},
		{"system", storedRecord(), func(r *Request) { r.Actor = memory.ActorSystem }, memory.DeletionActorNotAllowed},
		{"missing", RecordFacts{}, func(*Request) {}, memory.DeletionNotFound},
		{"foreign project", foreign, func(*Request) {}, memory.DeletionNotFound},
		{"no actor", storedRecord(), func(r *Request) { r.Actor = "" }, memory.DeletionInvalidFacts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{facts: tc.facts}
			request := humanRequest()
			tc.mutate(&request)
			got, err := Service{Store: store, NewID: func(string) (string, error) { t.Fatal("receipt id drawn"); return "", nil }}.DeleteRecord(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tc.want || got.Reason == "" || got.Receipts != nil || got.RecordVersion != 0 {
				t.Fatalf("result = %+v", got)
			}
			if store.transactions != 1 || len(store.deletions) != 0 || store.callbackErr != nil {
				t.Fatalf("transactions %d, deletions %d, callback %v", store.transactions, len(store.deletions), store.callbackErr)
			}
		})
	}
}

func TestDeleteRecordPropagatesFailures(t *testing.T) {
	readFailure, deleteFailure, beginFailure, idFailure := errors.New("read"), errors.New("write"), errors.New("begin"), errors.New("entropy")
	for _, tc := range []struct {
		name      string
		store     *fakeStore
		newID     func(string) (string, error)
		cause     error
		deletions int
	}{
		{"read", &fakeStore{facts: storedRecord(), readErr: readFailure}, sequentialIDs(), readFailure, 0},
		{"write", &fakeStore{facts: storedRecord(), deleteErr: deleteFailure}, sequentialIDs(), deleteFailure, 1},
		{"transaction", &fakeStore{facts: storedRecord(), withinErr: beginFailure}, sequentialIDs(), beginFailure, 0},
		{"receipt id", &fakeStore{facts: storedRecord()}, func(string) (string, error) { return "", idFailure }, idFailure, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Service{Store: tc.store, NewID: tc.newID}.DeleteRecord(context.Background(), humanRequest())
			if !errors.Is(err, tc.cause) || got.Kind != "" || got.Receipts != nil {
				t.Fatalf("result %+v, error %v", got, err)
			}
			if tc.name != "transaction" && !errors.Is(tc.store.callbackErr, tc.cause) {
				t.Fatalf("callback returned %v, so the store would commit", tc.store.callbackErr)
			}
			if len(tc.store.deletions) != tc.deletions {
				t.Fatalf("deletions = %d", len(tc.store.deletions))
			}
		})
	}
}

func TestDeleteRecordRequiresTime(t *testing.T) {
	store := &fakeStore{facts: storedRecord()}
	request := humanRequest()
	request.Now = time.Time{}
	if _, err := (Service{Store: store}).DeleteRecord(context.Background(), request); !errors.Is(err, ErrInvalidRequest) || store.transactions != 0 {
		t.Fatalf("error %v, transactions %d", err, store.transactions)
	}
}

func TestDeleteRecordDefaultsToRandomReceiptIDs(t *testing.T) {
	store := &fakeStore{facts: storedRecord()}
	got, err := Service{Store: store}.DeleteRecord(context.Background(), humanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Receipts) != 2 || got.Receipts[0].ID == got.Receipts[1].ID || !strings.HasPrefix(got.Receipts[0].ID, "deletion-") {
		t.Fatalf("receipts = %+v", got.Receipts)
	}
}
