package audit

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
)

type fakeSuggestions struct {
	page  SuggestionPage
	err   error
	after []SuggestionPosition
	limit int
}

func (f *fakeSuggestions) ReadAgentSuggestions(_ context.Context, projectID string, after SuggestionPosition, limit int) (SuggestionPage, error) {
	if projectID != "p1" {
		return SuggestionPage{}, errors.New("unexpected project")
	}
	f.after, f.limit = append(f.after, after), limit
	return f.page, f.err
}

// Only suggestions that still apply are listed; the cursor moves past every
// suggestion read, listed or not.
func TestPendingSuggestionsListsOnlyThoseThatStillApply(t *testing.T) {
	applies := storedSuggestion(storedRecord("r1", memory.StatusActive), memory.SuggestArchive)
	deleteArchived := storedSuggestion(storedRecord("r2", memory.StatusArchived), memory.SuggestDelete)
	stale := storedSuggestion(storedRecord("r3", memory.StatusActive), memory.SuggestDelete)
	stale.Standing.Record.CurrentVersionID = "r3-v2"
	moved := storedSuggestion(storedRecord("r4", memory.StatusArchived), memory.SuggestArchive)
	deleted := storedSuggestion(storedRecord("r5", memory.StatusActive), memory.SuggestDelete)
	deleted.Standing.CurrentContentDeleted = true
	foreign := storedSuggestion(storedRecord("r6", memory.StatusActive), memory.SuggestDelete)
	foreign.Standing.Record.ProjectID = "p2"
	last := storedSuggestion(storedRecord("r7", memory.StatusActive), memory.SuggestArchive)
	last.Standing.Record.Version = 2
	last.ReviewedAt = auditTime.Add(time.Minute)
	page := []StoredSuggestion{applies, stale, moved, deleteArchived, deleted, foreign, last}
	reader := &fakeSuggestions{page: SuggestionPage{Suggestions: page, More: true}}
	after := SuggestionPosition{ReviewedAt: auditTime.Add(-time.Hour), ReviewID: "review-0"}
	got, err := Service{Suggestions: reader}.PendingSuggestions(context.Background(), PendingRequest{ProjectID: "p1", After: after, Limit: 7})
	if err != nil {
		t.Fatal(err)
	}
	if reader.after[0] != after || reader.limit != 7 {
		t.Fatalf("read after %+v, limit %d", reader.after, reader.limit)
	}
	var ids []string
	for _, suggestion := range got.Suggestions {
		ids = append(ids, suggestion.ReviewID)
	}
	if !slices.Equal(ids, []string{"review-r1", "review-r2", "review-r7"}) {
		t.Fatalf("pending = %v", ids)
	}
	first := got.Suggestions[0]
	want := PendingSuggestion{
		ReviewID: "review-r1", Action: memory.SuggestArchive, Reason: "obsolete", Reviewer: "auditor", ReviewedAt: auditTime,
		VersionID: "r1-v", ContentHash: hashOf("r1"), Record: applies.Standing.Record,
	}
	if first != want || got.Suggestions[1].Action != memory.SuggestDelete || got.Suggestions[2].Record.Version != 2 {
		t.Fatalf("pending = %+v", got.Suggestions)
	}
	if got.Next != (SuggestionPosition{ReviewedAt: last.ReviewedAt, ReviewID: "review-r7"}) {
		t.Fatalf("next = %+v", got.Next)
	}

	// A page whose suggestions no longer apply is empty but still moves on.
	reader = &fakeSuggestions{page: SuggestionPage{Suggestions: []StoredSuggestion{stale}, More: true}}
	got, err = Service{Suggestions: reader}.PendingSuggestions(context.Background(), PendingRequest{ProjectID: "p1", Limit: 1})
	if err != nil || got.Suggestions != nil || got.Next.ReviewID != "review-r3" {
		t.Fatalf("pending %+v, error %v", got, err)
	}
	reader = &fakeSuggestions{page: SuggestionPage{Suggestions: []StoredSuggestion{applies}}}
	got, err = Service{Suggestions: reader}.PendingSuggestions(context.Background(), PendingRequest{ProjectID: "p1", Limit: MaxPendingPageSize})
	if err != nil || len(got.Suggestions) != 1 || got.Next != (SuggestionPosition{}) {
		t.Fatalf("pending %+v, error %v", got, err)
	}
}

func TestPendingSuggestionsRefusesInvalidRequests(t *testing.T) {
	reader := &fakeSuggestions{}
	for _, request := range []PendingRequest{{Limit: 1}, {ProjectID: "p1"}, {ProjectID: "p1", Limit: MaxPendingPageSize + 1}} {
		if _, err := (Service{Suggestions: reader}).PendingSuggestions(context.Background(), request); !errors.Is(err, ErrInvalidPendingRequest) {
			t.Fatalf("%+v: %v", request, err)
		}
	}
	if len(reader.after) != 0 {
		t.Fatal("reader was called")
	}
	failing := &fakeSuggestions{err: errInjected}
	if _, err := (Service{Suggestions: failing}).PendingSuggestions(context.Background(), PendingRequest{ProjectID: "p1", Limit: 1}); !errors.Is(err, errInjected) {
		t.Fatalf("error = %v", err)
	}
}
