package audit

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
)

func confirmRequest(reviewID string) ConfirmRequest {
	return ConfirmRequest{ProjectID: "p1", ReviewID: reviewID, ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "agreed", Now: auditTime}
}

// suggestionStore holds record id in status and the agent's suggestion of
// action about its current version.
func suggestionStore(id string, status memory.RecordStatus, action memory.Suggestion) *fakeStore {
	store := newFakeStore()
	record := storedRecord(id, status)
	store.addRecord(record)
	store.suggestions["review-"+id] = storedSuggestion(record, action)
	return store
}

// The one map from a suggestion to its action covers every suggestion.
func TestConfirmersCoverEverySuggestion(t *testing.T) {
	for _, suggestion := range memory.Suggestions() {
		if confirmers[suggestion] == nil {
			t.Fatalf("suggestion %s has no confirmer", suggestion)
		}
	}
	if len(confirmers) != len(memory.Suggestions()) {
		t.Fatalf("confirmers = %d", len(confirmers))
	}
}

// Confirming an archive suggestion archives the record through the archive
// transition, with the human's own reason, in the one transaction.
func TestConfirmArchive(t *testing.T) {
	store := suggestionStore("r1", memory.StatusNeedsVerification, memory.SuggestArchive)
	got, err := auditService(store).ConfirmSuggestion(context.Background(), confirmRequest("review-r1"))
	if err != nil || got.Kind != memory.SuggestionApplies || got.Action != memory.SuggestArchive || !got.Applied ||
		got.Transition.Kind != memory.TransitionAllowed || got.Transition.RecordVersion != 4 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if store.transactions != 1 || !slices.Equal(store.reads, []string{"suggestion review-r1", "record r1"}) {
		t.Fatalf("transactions %d, reads %v", store.transactions, store.reads)
	}
	wantWrites(t, store, "status r1 needs_verification->archived", "review r1-v opinion by human")
	if review := store.reviews[0]; review.Reason != "agreed" || review.Reviewer != "martin" || review.Suggestion != "" {
		t.Fatalf("review = %+v", review)
	}
}

func TestConfirmDelete(t *testing.T) {
	store := suggestionStore("r1", memory.StatusActive, memory.SuggestDelete)
	request := confirmRequest("review-r1")
	request.Reason = ""
	got, err := auditService(store).ConfirmSuggestion(context.Background(), request)
	if err != nil || got.Kind != memory.SuggestionApplies || got.Action != memory.SuggestDelete || !got.Applied ||
		got.Deletion.Kind != memory.DeletionDelete || got.Deletion.RecordVersion != 4 || len(got.Deletion.Receipts) != 1 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if !slices.Equal(store.reads, []string{"suggestion review-r1", "deletion r1"}) {
		t.Fatalf("reads = %v", store.reads)
	}
	wantWrites(t, store, "delete r1 1 versions")
	if d := store.deletions[0]; d.ProjectID != "p1" || d.ExpectedVersion != 3 || !d.DeletedAt.Equal(auditTime) {
		t.Fatalf("deletion = %+v", d)
	}
}

// Every refusal, of the suggestion or of its action, writes nothing.
func TestConfirmRefusalsWriteNothing(t *testing.T) {
	type setup func(*fakeStore, *ConfirmRequest)
	edit := func(change func(*StoredSuggestion)) setup {
		return func(s *fakeStore, _ *ConfirmRequest) {
			stored := s.suggestions["review-r1"]
			change(&stored)
			s.suggestions["review-r1"] = stored
		}
	}
	tests := []struct {
		name       string
		action     memory.Suggestion
		setup      setup
		kind       memory.SuggestionKind
		transition memory.RecordTransitionKind
		deletion   memory.RecordDeletionKind
	}{
		{"agent confirms", memory.SuggestArchive, func(_ *fakeStore, r *ConfirmRequest) { r.Actor = memory.ActorAgent }, memory.SuggestionActorNotAllowed, "", ""},
		{"system confirms", memory.SuggestDelete, func(_ *fakeStore, r *ConfirmRequest) { r.Actor = memory.ActorSystem }, memory.SuggestionActorNotAllowed, "", ""},
		{"unknown actor", memory.SuggestDelete, func(_ *fakeStore, r *ConfirmRequest) { r.Actor = "operator" }, memory.SuggestionInvalidInput, "", ""},
		{"no review id", memory.SuggestDelete, func(_ *fakeStore, r *ConfirmRequest) { r.ReviewID = "" }, memory.SuggestionInvalidInput, "", ""},
		{"no project", memory.SuggestDelete, func(_ *fakeStore, r *ConfirmRequest) { r.ProjectID = "" }, memory.SuggestionInvalidInput, "", ""},
		{"missing review", memory.SuggestArchive, func(_ *fakeStore, r *ConfirmRequest) { r.ReviewID = "review-x" }, memory.SuggestionNotFound, "", ""},
		{"another project", memory.SuggestArchive, func(_ *fakeStore, r *ConfirmRequest) { r.ProjectID = "p2" }, memory.SuggestionNotFound, "", ""},
		{"not a suggestion", memory.SuggestArchive, edit(func(s *StoredSuggestion) { s.Standing.Suggestion = "" }), memory.SuggestionNotFound, "", ""},
		{"human review", memory.SuggestArchive, edit(func(s *StoredSuggestion) {
			s.Standing.Actor, s.Standing.Decision = memory.ActorHuman, memory.ReviewApprove
		}), memory.SuggestionNotFound, "", ""},
		{"new version", memory.SuggestArchive, edit(func(s *StoredSuggestion) { s.Standing.Record.CurrentVersionID = "r1-v2" }), memory.SuggestionStale, "", ""},
		{"status moved", memory.SuggestArchive, edit(func(s *StoredSuggestion) { s.Standing.Record.Status = memory.StatusArchived }), memory.SuggestionStatusMoved, "", ""},
		{"content deleted", memory.SuggestDelete, edit(func(s *StoredSuggestion) { s.Standing.CurrentContentDeleted = true }), memory.SuggestionContentDeleted, "", ""},
		{"archive without the human's reason", memory.SuggestArchive, func(_ *fakeStore, r *ConfirmRequest) { r.Reason = " " }, memory.SuggestionApplies, memory.TransitionReasonRequired, ""},
		{"archive at another version", memory.SuggestArchive, func(_ *fakeStore, r *ConfirmRequest) { r.ExpectedVersion = 2 }, memory.SuggestionApplies, memory.TransitionVersionConflict, ""},
		{"delete at another version", memory.SuggestDelete, func(_ *fakeStore, r *ConfirmRequest) { r.ExpectedVersion = 4 }, memory.SuggestionApplies, "", memory.DeletionVersionConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := suggestionStore("r1", memory.StatusActive, tt.action)
			request := confirmRequest("review-r1")
			tt.setup(store, &request)
			got, err := auditService(store).ConfirmSuggestion(context.Background(), request)
			if err != nil || got.Kind != tt.kind || got.Reason == "" || got.Applied ||
				got.Transition.Kind != tt.transition || got.Deletion.Kind != tt.deletion {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantWrites(t, store)
		})
	}
}

func TestConfirmNeedsTheTimeAndAReviewer(t *testing.T) {
	store := suggestionStore("r1", memory.StatusActive, memory.SuggestArchive)
	for _, change := range []func(*ConfirmRequest){
		func(r *ConfirmRequest) { r.Now = time.Time{} },
		func(r *ConfirmRequest) { r.Reviewer = "" },
	} {
		request := confirmRequest("review-r1")
		change(&request)
		if _, err := auditService(store).ConfirmSuggestion(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("error = %v", err)
		}
	}
	if store.transactions != 0 {
		t.Fatalf("transactions = %d", store.transactions)
	}
}

// A failed read, write or transaction is returned with its cause and no
// result; the real store then rolls back.
func TestConfirmPropagatesFailures(t *testing.T) {
	tests := []struct {
		name   string
		action memory.Suggestion
		method string
	}{
		{"read suggestion", memory.SuggestArchive, "ReadSuggestion"},
		{"read record", memory.SuggestArchive, "ReadRecord"},
		{"change status", memory.SuggestArchive, "ChangeStatus"},
		{"insert review", memory.SuggestArchive, "InsertReview"},
		{"read deletion facts", memory.SuggestDelete, "ReadRecordDeletionFacts"},
		{"delete content", memory.SuggestDelete, "DeleteRecordContent"},
		{"transaction", memory.SuggestDelete, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := suggestionStore("r1", memory.StatusActive, tt.action)
			if tt.method == "" {
				store.failTransaction = 1
			} else {
				store.failures[tt.method] = errInjected
			}
			got, err := auditService(store).ConfirmSuggestion(context.Background(), confirmRequest("review-r1"))
			if !errors.Is(err, errInjected) || !reflect.DeepEqual(got, ConfirmResult{}) {
				t.Fatalf("result %+v, error %v", got, err)
			}
		})
	}
}

// An agent or system caller is refused from the request facts: neither the
// transaction nor the reader runs, so a failing read of an existing review
// cannot tell it apart from a missing one.
func TestConfirmSuggestionRefusesTheActorBeforeReading(t *testing.T) {
	for _, actor := range []memory.Actor{memory.ActorAgent, memory.ActorSystem} {
		for _, reviewID := range []string{"review-r1", "review-missing"} {
			store := suggestionStore("r1", memory.StatusActive, memory.SuggestArchive)
			store.failures["ReadSuggestion"] = errInjected
			store.failTransaction = 1
			request := confirmRequest(reviewID)
			request.Actor = actor
			got, err := auditService(store).ConfirmSuggestion(context.Background(), request)
			if err != nil || got.Kind != memory.SuggestionActorNotAllowed || got.Reason == "" {
				t.Fatalf("%s %s: result %+v, error %v", actor, reviewID, got, err)
			}
			if store.transactions != 0 || len(store.reads) != 0 {
				t.Fatalf("%s %s: transactions %d, reads %v", actor, reviewID, store.transactions, store.reads)
			}
		}
	}
}
