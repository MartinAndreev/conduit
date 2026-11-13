package proposals

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/reportblock"
)

func recordRequest(report string) RecordRequest {
	return RecordRequest{Report: report, ProjectID: "p1", RunID: "run", AttemptID: "a1", BaseCommit: "abc", Author: "codex", Now: reviewTime}
}

func TestRecordProposalsCreatesEachKeptProposal(t *testing.T) {
	store := newFakeRecords()
	report := proposalsOf(append([]string{
		`{"category":"gotcha","content":"Токен ` + fakeToken + `","sources":[{"kind":"file","path":"a.go","blob_hash":"388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"}]}`,
		entry("x", "невалиден"),
	}, distinctEntries(6)...)...)
	got, err := proposalService(store).RecordProposals(context.Background(), recordRequest(report))
	if err != nil {
		t.Fatal(err)
	}
	wantDropped := []DropCount{{Kind: memory.ProposalInvalidCategory, Count: 1}, {Kind: memory.ProposalOverLimit, Count: 2}}
	if got.Extraction.Block != reportblock.Read || !slices.Equal(got.Extraction.Dropped, wantDropped) ||
		!slices.Equal(got.Extraction.Findings, []redaction.Finding{{Kind: redaction.KindGitHubToken, Count: 1}}) {
		t.Fatalf("extraction = %+v", got.Extraction)
	}
	wantEntries := []int{1, 3, 4, 5, 6}
	for index, recorded := range got.Recorded {
		result := recorded.Result
		if recorded.Entry != wantEntries[index] || result.Kind != memory.CreationAllowed || result.Status != memory.StatusProposed || result.RecordID == "" || result.VersionID == "" || result.Findings != nil {
			t.Fatalf("recorded %d = %+v", index, recorded)
		}
	}
	if len(got.Recorded) != 5 || store.transactions != 5 {
		t.Fatalf("recorded %d in %d transactions", len(got.Recorded), store.transactions)
	}
	wantWrites(t, store,
		"insert record-1 proposed agent_claim attempt/a1", "insert record-3 proposed agent_claim attempt/a1", "insert record-5 proposed agent_claim attempt/a1",
		"insert record-7 proposed agent_claim attempt/a1", "insert record-9 proposed agent_claim attempt/a1",
	)
	first := store.created[0]
	wantScope := memory.Scope{ProjectID: "p1", Visibility: memory.VisibilityAttempt, Origin: memory.OriginAttempt, RunID: "run", AttemptID: "a1", BaseCommit: "abc"}
	if first.Version.Scope != wantScope || first.Version.Author != "codex" || first.Record.Category != memory.CategoryGotcha ||
		first.Version.Content != "Токен [REDACTED:github_token]" || !slices.Equal(first.Sources, []memory.RecordSource{{Kind: memory.SourceFile, Path: "a.go", BlobHash: "388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"}}) {
		t.Fatalf("first proposal = %+v", first)
	}
	if len(store.reviews) != 0 {
		t.Fatalf("reviews = %+v", store.reviews)
	}
}

func TestRecordProposalsWithoutABlockWritesNothing(t *testing.T) {
	for name, report := range map[string]string{
		"absent":    "Готово.",
		"multiple":  proposalsOf(entry("fact", "a")) + proposalsOf(entry("fact", "b")),
		"malformed": block(`{"proposals":`),
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeRecords()
			got, err := proposalService(store).RecordProposals(context.Background(), recordRequest(report))
			if err != nil || got.Recorded != nil || got.Extraction.Block == reportblock.Read || store.transactions != 0 {
				t.Fatalf("result %+v, error %v", got, err)
			}
		})
	}
}

// A creation refusal is that proposal's result, not a drop, and keeps its
// place within the limit.
func TestRecordProposalsReportsCreationRefusals(t *testing.T) {
	store := newFakeRecords()
	report := proposalsOf(entry("fact", strings.Repeat("ж", memory.MaxStoredContentBytes/2+1)), entry("fact", "кратко"))
	got, err := proposalService(store).RecordProposals(context.Background(), recordRequest(report))
	if err != nil || len(got.Recorded) != 2 || got.Extraction.Dropped != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if got.Recorded[0].Result.Kind != memory.CreationContentTooLarge || got.Recorded[0].Result.RecordID != "" || got.Recorded[1].Result.Kind != memory.CreationAllowed {
		t.Fatalf("recorded = %+v", got.Recorded)
	}
	wantWrites(t, store, "insert record-1 proposed agent_claim attempt/a1")

	incomplete := recordRequest(proposalsOf(entry("fact", "a")))
	incomplete.BaseCommit = ""
	store = newFakeRecords()
	got, err = proposalService(store).RecordProposals(context.Background(), incomplete)
	if err != nil || len(got.Recorded) != 1 || got.Recorded[0].Result.Kind != memory.CreationInvalidScope || len(store.writes) != 0 {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

// Creation stops at the first error; the proposals before it stay recorded
// and are reported.
func TestRecordProposalsStopsAtAnError(t *testing.T) {
	store := newFakeRecords()
	store.failTransaction = 2
	got, err := proposalService(store).RecordProposals(context.Background(), recordRequest(proposalsOf(distinctEntries(3)...)))
	if !errors.Is(err, errInjected) || !strings.Contains(err.Error(), "entry 2") {
		t.Fatalf("error = %v", err)
	}
	if len(got.Recorded) != 1 || got.Recorded[0].Entry != 1 || len(got.Extraction.Proposals) != 3 || store.transactions != 2 {
		t.Fatalf("result = %+v after %d transactions", got, store.transactions)
	}
}

// Cancellation is checked before the report is read at all.
func TestRecordProposalsStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := newFakeRecords()
	got, err := proposalService(store).RecordProposals(ctx, recordRequest(proposalsOf(distinctEntries(2)...)))
	if !errors.Is(err, context.Canceled) || got.Recorded != nil || store.transactions != 0 || got.Extraction.Block != "" {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

// Cancellation between creations stops before the next one; the recorded
// proposal is reported.
func TestRecordProposalsStopsWhenCancelledBetweenCreations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := newFakeRecords()
	store.afterTransaction = cancel
	got, err := proposalService(store).RecordProposals(ctx, recordRequest(proposalsOf(distinctEntries(2)...)))
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "entry 2") || len(got.Recorded) != 1 || store.transactions != 1 {
		t.Fatalf("result %+v, error %v", got, err)
	}
}

func TestRecordProposalsNeedsTheTime(t *testing.T) {
	store := newFakeRecords()
	request := recordRequest(proposalsOf(entry("fact", "a")))
	request.Now = time.Time{}
	if _, err := proposalService(store).RecordProposals(context.Background(), request); !errors.Is(err, ErrInvalidRequest) || store.transactions != 0 {
		t.Fatalf("error = %v", err)
	}
}

// An over-long path from an agent passes the keep rule's shape check and is
// that proposal's creation refusal; the later proposals are still recorded.
func TestRecordProposalsRefusesAnOverLongSourcePath(t *testing.T) {
	store := newFakeRecords()
	long := `{"category":"fact","content":"дълъг път","sources":[{"kind":"file","path":"` + strings.Repeat("a", memory.MaxTextFieldBytes+1) + `","blob_hash":"0fd0bcfb44f83e7d5ac7a8922578276b9af48746"}]}`
	got, err := proposalService(store).RecordProposals(context.Background(), recordRequest(proposalsOf(long, entry("fact", "следващо"))))
	if err != nil || got.Extraction.Dropped != nil || len(got.Recorded) != 2 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if refused := got.Recorded[0]; refused.Entry != 1 || refused.Result.Kind != memory.CreationInvalidSource || refused.Result.RecordID != "" {
		t.Fatalf("refused = %+v", refused)
	}
	if recorded := got.Recorded[1]; recorded.Entry != 2 || recorded.Result.Kind != memory.CreationAllowed {
		t.Fatalf("recorded = %+v", recorded)
	}
	wantWrites(t, store, "insert record-1 proposed agent_claim attempt/a1")
}

// A report over memory.MaxReportBytes records nothing and returns its
// extraction; exactly the limit is recorded.
func TestRecordProposalsRefusesATooLargeReport(t *testing.T) {
	valid := proposalsOf(entry("fact", "a"))
	padded := func(size int) string {
		return strings.Repeat("x", size-len(valid)-1) + "\n" + valid
	}
	store := newFakeRecords()
	got, err := proposalService(store).RecordProposals(context.Background(), recordRequest(padded(memory.MaxReportBytes+1)))
	if err != nil || got.Recorded != nil || got.Extraction.Block != reportblock.TooLarge || got.Extraction.Entries != 0 || store.transactions != 0 {
		t.Fatalf("result %+v, error %v", got, err)
	}
	got, err = proposalService(store).RecordProposals(context.Background(), recordRequest(padded(memory.MaxReportBytes)))
	if err != nil || len(got.Recorded) != 1 || got.Recorded[0].Result.Kind != memory.CreationAllowed || store.transactions != 1 {
		t.Fatalf("result %+v, error %v", got, err)
	}
}
