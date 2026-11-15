package audit

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

func dismissRequest(reviewID string) DismissRequest {
	return DismissRequest{ProjectID: "p1", ReviewID: reviewID, ExpectedVersion: 3, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "still needed", Now: auditTime}
}

// A human's dismissal writes only the dismissal review, bound to the
// suggestion's version, content hash and action, with the redacted reason,
// in one transaction.
func TestDismissSuggestion(t *testing.T) {
	for _, action := range memory.Suggestions() {
		t.Run(string(action), func(t *testing.T) {
			store := suggestionStore("r1", memory.StatusActive, action)
			request := dismissRequest("review-r1")
			request.Reason = "нужно е: token=hunter2"
			request.Now = auditTime.In(time.FixedZone("EET", 2*60*60))
			got, err := auditService(store).DismissSuggestion(context.Background(), request)
			if err != nil || got.Kind != memory.SuggestionDismiss || got.Action != action || got.DismissalID != "review-1" || got.Reason == "" || len(got.Findings) != 1 {
				t.Fatalf("result %+v, error %v", got, err)
			}
			if store.transactions != 1 || !slices.Equal(store.reads, []string{"suggestion review-r1"}) {
				t.Fatalf("transactions %d, reads %v", store.transactions, store.reads)
			}
			wantWrites(t, store, "review r1-v dismiss by human suggests "+string(action))
			want := records.Review{
				ID: "review-1", VersionID: "r1-v", ContentHash: hashOf("r1"), Decision: memory.ReviewDismiss, Actor: memory.ActorHuman,
				Reviewer: "martin", Reason: "нужно е: token=[REDACTED:secret_assignment]", Suggestion: action, ReviewedAt: auditTime,
			}
			if review := store.reviews[0]; review != want || review.ReviewedAt.Location() != time.UTC {
				t.Fatalf("review = %+v", review)
			}
		})
	}
}

func TestDismissSuggestionWithoutAReason(t *testing.T) {
	store := suggestionStore("r1", memory.StatusArchived, memory.SuggestDelete)
	request := dismissRequest("review-r1")
	request.Reason = ""
	got, err := auditService(store).DismissSuggestion(context.Background(), request)
	if err != nil || got.Kind != memory.SuggestionDismiss || got.Findings != nil || store.reviews[0].Reason != "" {
		t.Fatalf("result %+v, error %v, reviews %+v", got, err, store.reviews)
	}
}

// Without a configured generator the dismissal gets a generated review ID.
func TestDismissSuggestionGeneratesItsID(t *testing.T) {
	store := suggestionStore("r1", memory.StatusActive, memory.SuggestArchive)
	service := auditService(store)
	service.NewID = nil
	got, err := service.DismissSuggestion(context.Background(), dismissRequest("review-r1"))
	if err != nil || !strings.HasPrefix(got.DismissalID, "review") || got.DismissalID == "review-1" || store.reviews[0].ID != got.DismissalID {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

// Every refusal writes nothing.
func TestDismissRefusalsWriteNothing(t *testing.T) {
	type setup func(*fakeStore, *DismissRequest)
	edit := func(change func(*StoredSuggestion)) setup {
		return func(s *fakeStore, _ *DismissRequest) {
			stored := s.suggestions["review-r1"]
			change(&stored)
			s.suggestions["review-r1"] = stored
		}
	}
	tests := []struct {
		name   string
		action memory.Suggestion
		setup  setup
		kind   memory.SuggestionDismissalKind
	}{
		{"agent dismisses", memory.SuggestArchive, func(_ *fakeStore, r *DismissRequest) { r.Actor = memory.ActorAgent }, memory.SuggestionDismissalActorNotAllowed},
		{"system dismisses", memory.SuggestDelete, func(_ *fakeStore, r *DismissRequest) { r.Actor = memory.ActorSystem }, memory.SuggestionDismissalActorNotAllowed},
		{"unknown actor", memory.SuggestDelete, func(_ *fakeStore, r *DismissRequest) { r.Actor = "operator" }, memory.SuggestionDismissalInvalidFacts},
		{"no review id", memory.SuggestDelete, func(_ *fakeStore, r *DismissRequest) { r.ReviewID = "" }, memory.SuggestionDismissalInvalidFacts},
		{"no project", memory.SuggestDelete, func(_ *fakeStore, r *DismissRequest) { r.ProjectID = "" }, memory.SuggestionDismissalInvalidFacts},
		{"reviewer over the bound", memory.SuggestDelete, func(_ *fakeStore, r *DismissRequest) {
			r.Reviewer = strings.Repeat("m", memory.MaxIdentityBytes+1)
		}, memory.SuggestionDismissalInvalidFacts},
		{"missing review", memory.SuggestArchive, func(_ *fakeStore, r *DismissRequest) { r.ReviewID = "review-x" }, memory.SuggestionDismissalNotFound},
		{"another project", memory.SuggestArchive, func(_ *fakeStore, r *DismissRequest) { r.ProjectID = "p2" }, memory.SuggestionDismissalNotFound},
		{"a dismissal is not a suggestion", memory.SuggestArchive, edit(func(s *StoredSuggestion) {
			s.Standing.Actor, s.Standing.Decision = memory.ActorHuman, memory.ReviewDismiss
		}), memory.SuggestionDismissalNotFound},
		{"new version", memory.SuggestArchive, edit(func(s *StoredSuggestion) { s.Standing.Record.CurrentVersionID = "r1-v2" }), memory.SuggestionDismissalStale},
		{"already dismissed", memory.SuggestDelete, edit(func(s *StoredSuggestion) { s.Standing.Dismissed = true }), memory.SuggestionAlreadyDismissed},
		{"content deleted", memory.SuggestDelete, edit(func(s *StoredSuggestion) { s.Standing.CurrentContentDeleted = true }), memory.SuggestionDismissalContentDeleted},
		{"status moved", memory.SuggestArchive, edit(func(s *StoredSuggestion) { s.Standing.Record.Status = memory.StatusArchived }), memory.SuggestionDismissalStatusMoved},
		{"another expected version", memory.SuggestArchive, func(_ *fakeStore, r *DismissRequest) { r.ExpectedVersion = 2 }, memory.SuggestionDismissalVersionConflict},
		{"no expected version", memory.SuggestArchive, func(_ *fakeStore, r *DismissRequest) { r.ExpectedVersion = 0 }, memory.SuggestionDismissalInvalidFacts},
		{"reason over the bound", memory.SuggestArchive, func(_ *fakeStore, r *DismissRequest) {
			r.Reason = strings.Repeat("ж", memory.MaxTextFieldBytes/2+1)
		}, memory.SuggestionDismissalReasonTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := suggestionStore("r1", memory.StatusActive, tt.action)
			request := dismissRequest("review-r1")
			tt.setup(store, &request)
			got, err := auditService(store).DismissSuggestion(context.Background(), request)
			if err != nil || got.Kind != tt.kind || got.Reason == "" || got.Action != "" || got.DismissalID != "" {
				t.Fatalf("result %+v, error %v", got, err)
			}
			wantWrites(t, store)
		})
	}
}

func TestDismissNeedsTheTimeAndAReviewer(t *testing.T) {
	store := suggestionStore("r1", memory.StatusActive, memory.SuggestArchive)
	for _, change := range []func(*DismissRequest){
		func(r *DismissRequest) { r.Now = time.Time{} },
		func(r *DismissRequest) { r.Reviewer = "" },
	} {
		request := dismissRequest("review-r1")
		change(&request)
		if _, err := auditService(store).DismissSuggestion(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("error = %v", err)
		}
	}
	if store.transactions != 0 {
		t.Fatalf("transactions = %d", store.transactions)
	}
}

// A failed read, ID, write or transaction is returned with its cause and no
// result; the real store then rolls back.
func TestDismissPropagatesFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeStore, *Service)
	}{
		{"read suggestion", func(s *fakeStore, _ *Service) { s.failures["ReadSuggestion"] = errInjected }},
		{"insert review", func(s *fakeStore, _ *Service) { s.failures["InsertReview"] = errInjected }},
		{"transaction", func(s *fakeStore, _ *Service) { s.failTransaction = 1 }},
		{"id", func(_ *fakeStore, service *Service) {
			service.NewID = func(string) (string, error) { return "", errInjected }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := suggestionStore("r1", memory.StatusActive, memory.SuggestArchive)
			service := auditService(store)
			tt.setup(store, &service)
			got, err := service.DismissSuggestion(context.Background(), dismissRequest("review-r1"))
			if !errors.Is(err, errInjected) || !reflect.DeepEqual(got, DismissResult{}) {
				t.Fatalf("result %+v, error %v", got, err)
			}
		})
	}
}

// An agent or system caller is refused from the request facts: neither the
// transaction nor the reader runs, so a failing read of an existing review
// cannot tell it apart from a missing one.
func TestDismissSuggestionRefusesTheActorBeforeReading(t *testing.T) {
	for _, actor := range []memory.Actor{memory.ActorAgent, memory.ActorSystem} {
		for _, reviewID := range []string{"review-r1", "review-missing"} {
			store := suggestionStore("r1", memory.StatusActive, memory.SuggestArchive)
			store.failures["ReadSuggestion"] = errInjected
			store.failTransaction = 1
			request := dismissRequest(reviewID)
			request.Actor = actor
			got, err := auditService(store).DismissSuggestion(context.Background(), request)
			if err != nil || got.Kind != memory.SuggestionDismissalActorNotAllowed || got.Reason == "" {
				t.Fatalf("%s %s: result %+v, error %v", actor, reviewID, got, err)
			}
			if store.transactions != 0 || len(store.reads) != 0 {
				t.Fatalf("%s %s: transactions %d, reads %v", actor, reviewID, store.transactions, store.reads)
			}
		}
	}
}
