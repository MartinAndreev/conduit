package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/reportblock"
)

func recordRequest(report string) RecordRequest {
	return RecordRequest{Report: report, ProjectID: "p1", Reviewer: "auditor", Now: auditTime}
}

// Each kept suggestion is the agent's opinion with its suggestion, in its
// own transaction; a refused one is that suggestion's result and the run
// continues.
func TestRecordSuggestionsAddsEachKeptSuggestion(t *testing.T) {
	store := newFakeStore()
	store.addRecord(storedRecord("r1", memory.StatusActive))
	moved := storedRecord("r2", memory.StatusArchived)
	store.addRecord(moved)
	store.addRecord(storedRecord("r3", memory.StatusNeedsVerification))
	report := suggestionsOf(
		entry("r1", "archive", "дублира r3 password: hunter2"),
		entry("r2", "archive", "остаряло"),
		entry("r3", "delete", "грешно"),
		entry("r4", "delete", "липсва"),
		entry("r5", "forget", "x"),
	)
	got, err := auditService(store).RecordSuggestions(context.Background(), recordRequest(report))
	if err != nil {
		t.Fatal(err)
	}
	if got.Extraction.Block != reportblock.Read || len(got.Extraction.Suggestions) != 4 || len(got.Extraction.Findings) != 1 ||
		len(got.Extraction.Dropped) != 1 || got.Extraction.Dropped[0].Kind != memory.SuggestionUnknownAction {
		t.Fatalf("extraction = %+v", got.Extraction)
	}
	want := []struct {
		entry int
		kind  memory.RecordOpinionKind
	}{{1, memory.OpinionAdd}, {2, memory.OpinionStatusNotApplicable}, {3, memory.OpinionAdd}, {4, memory.OpinionNotFound}}
	if len(got.Recorded) != len(want) {
		t.Fatalf("recorded = %+v", got.Recorded)
	}
	for index, w := range want {
		if got.Recorded[index].Entry != w.entry || got.Recorded[index].Result.Kind != w.kind {
			t.Fatalf("recorded %d = %+v, want %+v", index, got.Recorded[index], w)
		}
	}
	if store.transactions != 4 {
		t.Fatalf("transactions = %d", store.transactions)
	}
	wantWrites(t, store, "review r1-v opinion by agent suggests archive", "review r3-v opinion by agent suggests delete")
	first := store.reviews[0]
	if first.Reviewer != "auditor" || first.ContentHash != hashOf("r1") || first.Reason != "дублира r3 password: [REDACTED:secret_assignment]" || !first.ReviewedAt.Equal(auditTime) {
		t.Fatalf("review = %+v", first)
	}
}

func TestRecordSuggestionsWithoutABlockWritesNothing(t *testing.T) {
	for name, report := range map[string]string{
		"no block":  "Нищо.",
		"malformed": block(`{"suggestions":{}}`),
		"too large": suggestionsOf(entry("r1", "archive", "x")) + strings.Repeat("x", memory.MaxReportBytes),
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			got, err := auditService(store).RecordSuggestions(context.Background(), recordRequest(report))
			if err != nil || got.Recorded != nil || store.transactions != 0 {
				t.Fatalf("result %+v, error %v, transactions %d", got, err, store.transactions)
			}
		})
	}
}

// A reason over the text field bound after redaction is the opinion rule's
// refusal, not a drop.
func TestRecordSuggestionsReportsAnOverLongReason(t *testing.T) {
	store := newFakeStore()
	store.addRecord(storedRecord("r1", memory.StatusActive))
	report := suggestionsOf(entry("r1", "archive", strings.Repeat("ж", memory.MaxTextFieldBytes/2+1)))
	got, err := auditService(store).RecordSuggestions(context.Background(), recordRequest(report))
	if err != nil || len(got.Recorded) != 1 || got.Recorded[0].Result.Kind != memory.OpinionReasonTooLong || got.Extraction.Dropped != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
	wantWrites(t, store)
}

func TestRecordSuggestionsStopsAtAnError(t *testing.T) {
	store := newFakeStore()
	store.addRecord(storedRecord("r1", memory.StatusActive))
	store.addRecord(storedRecord("r2", memory.StatusActive))
	store.failTransaction = 2
	report := suggestionsOf(entry("r1", "archive", "a"), entry("r2", "archive", "b"), entry("r3", "archive", "c"))
	got, err := auditService(store).RecordSuggestions(context.Background(), recordRequest(report))
	if !errors.Is(err, errInjected) || !strings.Contains(err.Error(), "entry 2") || len(got.Recorded) != 1 || got.Recorded[0].Entry != 1 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if store.transactions != 2 {
		t.Fatalf("transactions = %d", store.transactions)
	}
}

func TestRecordSuggestionsStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := newFakeStore()
	got, err := auditService(store).RecordSuggestions(ctx, recordRequest(suggestionsOf(entry("r1", "archive", "a"))))
	if !errors.Is(err, context.Canceled) || got.Extraction.Block != "" || store.transactions != 0 {
		t.Fatalf("result %+v, error %v", got, err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	store = newFakeStore()
	store.addRecord(storedRecord("r1", memory.StatusActive))
	store.afterTransaction = cancel
	got, err = auditService(store).RecordSuggestions(ctx, recordRequest(suggestionsOf(entry("r1", "archive", "a"), entry("r2", "delete", "b"))))
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "entry 2") || len(got.Recorded) != 1 || store.transactions != 1 {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

func TestRecordSuggestionsNeedsTheTime(t *testing.T) {
	store := newFakeStore()
	request := recordRequest(suggestionsOf(entry("r1", "archive", "a")))
	request.Now = time.Time{}
	if _, err := auditService(store).RecordSuggestions(context.Background(), request); !errors.Is(err, ErrInvalidRequest) || store.transactions != 0 {
		t.Fatalf("error %v", err)
	}
}

// A suggestion a human dismissed for the version, content hash and action is
// that entry's suggestion_dismissed refusal, whose reason repeats no report
// value; the rest of the report is recorded, including the other action for
// the same version in a later report.
func TestRecordSuggestionsRefusesADismissedSuggestion(t *testing.T) {
	store := newFakeStore()
	store.addRecord(storedRecord("r1", memory.StatusActive))
	store.addRecord(storedRecord("r2", memory.StatusActive))
	store.dismissed["r1-v|"+hashOf("r1")+"|archive"] = true
	got, err := auditService(store).RecordSuggestions(context.Background(), recordRequest(suggestionsOf(
		entry("r1", "archive", "отново"),
		entry("r2", "archive", "остаряло"),
	)))
	if err != nil || len(got.Recorded) != 2 || got.Recorded[0].Result.Kind != memory.OpinionSuggestionDismissed || got.Recorded[1].Result.Kind != memory.OpinionAdd {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if refusal := got.Recorded[0].Result.Reason; strings.Contains(refusal, "r1") || strings.Contains(refusal, "отново") || strings.Contains(refusal, hashOf("r1")) {
		t.Fatalf("refusal reason repeats the report: %q", refusal)
	}
	wantWrites(t, store, "review r2-v opinion by agent suggests archive")

	other, err := auditService(store).RecordSuggestions(context.Background(), recordRequest(suggestionsOf(entry("r1", "delete", "изтрий"))))
	if err != nil || len(other.Recorded) != 1 || other.Recorded[0].Result.Kind != memory.OpinionAdd {
		t.Fatalf("other action %+v, error %v", other, err)
	}
}
