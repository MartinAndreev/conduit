package turso

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/memory/proposals"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	tursogo "turso.tech/database/tursogo"
)

// fakeGitHubToken has a token shape the redaction policy masks in place.
const fakeGitHubToken = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"

func proposalService(store *Store, files snapshotFiles) proposals.Service {
	return proposals.Service{Records: recordService(store, files), Normalizer: store.SearchNormalizer(), Queue: store.ProposalQueue()}
}

func agentReport(entries ...string) string {
	return "Готово.\n\n```conduit-memory\n{\"proposals\":[" + strings.Join(entries, ",\n") + "]}\n```\n"
}

func reportEntry(category, content string, sources ...string) string {
	return fmt.Sprintf(`{"category":%q,"content":%q,"sources":[%s]}`, category, content, strings.Join(sources, ","))
}

func attemptReport(report, attemptID string, at time.Time) proposals.RecordRequest {
	return proposals.RecordRequest{Report: report, ProjectID: "project", RunID: "run", AttemptID: attemptID, BaseCommit: "abc", Author: "codex", Now: at}
}

func mustRecordProposals(t *testing.T, store *Store, request proposals.RecordRequest) []string {
	t.Helper()
	got, err := proposalService(store, nil).RecordProposals(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(got.Recorded))
	for index, recorded := range got.Recorded {
		if recorded.Result.Kind != memory.CreationAllowed {
			t.Fatalf("recorded = %+v", recorded)
		}
		ids[index] = recorded.Result.RecordID
	}
	return ids
}

func queuePage(t *testing.T, store *Store, after records.ProposalPosition, limit int) proposals.Queue {
	t.Helper()
	got, err := proposalService(store, nil).ReviewQueue(context.Background(), proposals.QueueRequest{ProjectID: "project", After: after, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func entryIDs(queue proposals.Queue) []string {
	ids := make([]string, len(queue.Entries))
	for index, entry := range queue.Entries {
		ids[index] = entry.Item.Record.ID
	}
	return ids
}

func TestRecordProposalsFromAReport(t *testing.T) {
	store := openMemoryStore(t)
	report := agentReport(
		reportEntry("gotcha", "Неуспешен COMMIT оставя транзакцията отворена; токен "+fakeGitHubToken,
			`{"kind":"file","path":"internal/storage/turso/memory_write.go","blob_hash":"4bff34222a43b71631d0bc0a180aa03863cef75b"}`, `{"kind":"spec","ref":"specs/012"}`),
		reportEntry("fact", "неуспешен commit оставя транзакцията отворена; токен "+fakeGitHubToken),
		reportEntry("convention", "Времената се пазят в UTC"),
		reportEntry("architecture", "x"),
	)
	got, err := proposalService(store, nil).RecordProposals(context.Background(), attemptReport(report, "attempt-1", recordsNow))
	if err != nil {
		t.Fatal(err)
	}
	wantDropped := []proposals.DropCount{{Kind: memory.ProposalInvalidCategory, Count: 1}, {Kind: memory.ProposalDuplicate, Count: 1}}
	if !slices.Equal(got.Extraction.Dropped, wantDropped) || len(got.Recorded) != 2 ||
		!slices.Equal(got.Extraction.Findings, []redaction.Finding{{Kind: redaction.KindGitHubToken, Count: 2}}) {
		t.Fatalf("result = %+v", got)
	}
	rows := queryStrings(t, store, `SELECT r.status || '|' || r.category || '|' || v.trust || '|' || v.visibility || '|' || v.origin || '|' || v.attempt_id || '|' || v.author || '|' || v.content
		FROM memory_records r JOIN memory_record_versions v ON v.id = r.current_version_id ORDER BY r.category`)
	want := []string{
		"proposed|convention|agent_claim|attempt|attempt|attempt-1|codex|Времената се пазят в UTC",
		"proposed|gotcha|agent_claim|attempt|attempt|attempt-1|codex|Неуспешен COMMIT оставя транзакцията отворена; токен [REDACTED:github_token]",
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows = %q", rows)
	}
	if reviews := queryStrings(t, store, "SELECT id FROM memory_reviews"); len(reviews) != 0 {
		t.Fatalf("reviews = %v", reviews)
	}
	queue := queuePage(t, store, records.ProposalPosition{}, 10)
	if len(queue.Entries) != 2 || queue.Next != (records.ProposalPosition{}) {
		t.Fatalf("queue = %+v", queue)
	}
	for _, entry := range queue.Entries {
		withSources := entry.Item.Record.Category == memory.CategoryGotcha
		if entry.NoSource == withSources || (len(entry.Item.Sources) == 2) != withSources || entry.Item.Trust != memory.TrustAgentClaim ||
			entry.Item.Scope.AttemptID != "attempt-1" || entry.Item.ContentHash != memory.ContentHash(entry.Item.Content) {
			t.Fatalf("entry = %+v", entry)
		}
	}
}

// The queue holds only the project's proposals with content, ordered by
// (created_at, id), one page at a time.
func TestReviewQueuePagesThroughProposals(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	var ids []string
	for minute := range 3 {
		at := recordsNow.Add(time.Duration(minute) * time.Minute)
		ids = append(ids, mustRecordProposals(t, store, attemptReport(agentReport(
			reportEntry("fact", fmt.Sprintf("първо %d", minute)), reportEntry("fact", fmt.Sprintf("второ %d", minute))), "attempt-1", at))...)
	}
	mustCreate(t, store, humanText("Активен запис"))
	foreign := attemptReport(agentReport(reportEntry("fact", "чужд")), "attempt-1", recordsNow)
	foreign.ProjectID = "other"
	mustRecordProposals(t, store, foreign)
	if _, err := (deletion.Service{Store: store.MemoryDeletion()}).DeleteRecord(ctx, deletion.Request{ProjectID: "project", RecordID: ids[2], ExpectedVersion: 1, Actor: memory.ActorHuman, Now: recordsNow}); err != nil {
		t.Fatal(err)
	}
	if got := queryStrings(t, store, "SELECT status FROM memory_records WHERE id = ?", ids[2]); !slices.Equal(got, []string{"proposed"}) {
		t.Fatalf("tombstoned proposal status = %v", got)
	}
	// Two proposals of one report share created_at and are ordered by id.
	want := slices.Concat(slices.Sorted(slices.Values(ids[0:2])), []string{ids[3]}, slices.Sorted(slices.Values(ids[4:6])))
	var seen []string
	after := records.ProposalPosition{}
	for page := 0; ; page++ {
		queue := queuePage(t, store, after, 2)
		seen = append(seen, entryIDs(queue)...)
		if queue.Next == (records.ProposalPosition{}) {
			if page != 2 || len(queue.Entries) != 1 {
				t.Fatalf("last page %d = %+v", page, queue)
			}
			break
		}
		after = queue.Next
	}
	if !slices.Equal(seen, want) {
		t.Fatalf("pages = %v, want %v", seen, want)
	}
	exact := queuePage(t, store, records.ProposalPosition{}, 5)
	if len(exact.Entries) != 5 || exact.Next != (records.ProposalPosition{}) {
		t.Fatalf("exact page = %+v", exact)
	}
}

// Duplicates are exact matches of normalized text in one scope; another
// attempt gives a merge hint, and another normalization version nothing.
func TestReviewQueueGroupsDuplicates(t *testing.T) {
	store := openMemoryStore(t)
	first := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "Памет за ТЕСТ")), "attempt-1", recordsNow))
	second := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("gotcha", "памет за тест")), "attempt-1", recordsNow.Add(time.Minute)))
	other := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "ПАМЕТ ЗА ТЕСТ")), "attempt-2", recordsNow.Add(2*time.Minute)))
	renormalized := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "памет за тест")), "attempt-1", recordsNow.Add(3*time.Minute)))
	distinct := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "памет за тестове")), "attempt-1", recordsNow.Add(4*time.Minute)))
	mustExec(t, store, "UPDATE memory_record_versions SET search_normalization_version = 2 WHERE record_id = ?", renormalized[0])
	queue := queuePage(t, store, records.ProposalPosition{}, 10)
	want := map[string][2][]string{
		first[0]:        {second, other},
		second[0]:       {first, other},
		other[0]:        {nil, slices.Concat(first, second)},
		renormalized[0]: {nil, nil},
		distinct[0]:     {nil, nil},
	}
	if len(queue.Entries) != len(want) {
		t.Fatalf("queue = %+v", queue)
	}
	for _, entry := range queue.Entries {
		groups := want[entry.Item.Record.ID]
		if !slices.Equal(entry.Duplicates, groups[0]) || !slices.Equal(entry.MergeHints, groups[1]) {
			t.Fatalf("entry %s: duplicates %v, hints %v", entry.Item.Record.ID, entry.Duplicates, entry.MergeHints)
		}
	}
	if entry := queue.Entries[3]; entry.Item.SearchText.NormalizationVersion != 2 || entry.Item.SearchText.Digest != queue.Entries[1].Item.SearchText.Digest {
		t.Fatalf("renormalized identity = %+v", entry.Item.SearchText)
	}
}

func mergeBatch(content string, merged ...records.MergedProposal) proposals.BatchRequest {
	return proposals.BatchRequest{
		ProjectID: "project", Actor: memory.ActorHuman, Reviewer: "martin", Now: recordsNow.Add(time.Hour),
		Decisions: []proposals.Decision{{Kind: proposals.DecisionMerge, Merge: proposals.MergeDecision{
			Proposals: merged, Category: memory.CategoryConvention, Content: content,
			Sources: []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}},
		}}},
	}
}

func TestMergeProposalsIntoAHumanRecord(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	ids := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "Първо"), reportEntry("fact", "Второ")), "attempt-1", recordsNow))
	got, err := proposalService(store, nil).ReviewBatch(ctx, mergeBatch("Обединено правило",
		records.MergedProposal{RecordID: ids[0], ExpectedVersion: 1}, records.MergedProposal{RecordID: ids[1], ExpectedVersion: 1}))
	if err != nil || len(got.Results) != 1 || !got.Results[0].Applied {
		t.Fatalf("result %+v, error %v", got, err)
	}
	merged := got.Results[0].Merge
	rows := queryStrings(t, store, `SELECT r.id || '|' || r.status || '|' || CAST(r.version AS TEXT) || '|' || v.trust || '|' || v.origin || '|' || v.visibility
		FROM memory_records r JOIN memory_record_versions v ON v.id = r.current_version_id ORDER BY r.created_at, r.id`)
	want := slices.Sorted(slices.Values([]string{ids[0] + "|rejected|2|agent_claim|attempt|attempt", ids[1] + "|rejected|2|agent_claim|attempt|attempt"}))
	want = append(want, merged.RecordID+"|active|1|human_approved|human|project")
	if !slices.Equal(rows, want) {
		t.Fatalf("records = %q, want %q", rows, want)
	}
	reviews := queryStrings(t, store, "SELECT version_id || '|' || decision || '|' || actor || '|' || reviewer || '|' || COALESCE(reason, '') FROM memory_reviews ORDER BY decision, version_id")
	wantReviews := []string{merged.VersionID + "|approve|human|martin|"}
	for _, id := range slices.Sorted(slices.Values(ids)) {
		version := queryStrings(t, store, "SELECT current_version_id FROM memory_records WHERE id = ?", id)[0]
		wantReviews = append(wantReviews, version+"|reject|human|martin|merged into record "+merged.RecordID)
	}
	slices.Sort(wantReviews[1:])
	if !slices.Equal(reviews, wantReviews) {
		t.Fatalf("reviews = %q, want %q", reviews, wantReviews)
	}
	if relations := queryStrings(t, store, "SELECT kind FROM memory_relations"); len(relations) != 0 {
		t.Fatalf("relations = %v", relations)
	}
	if trust, ok := itemTrust(composeFor(t, store, nil, "обединено"), merged.RecordID); !ok || trust != memory.TrustHumanApproved {
		t.Fatal("merged record is not eligible")
	}
	if queue := queuePage(t, store, records.ProposalPosition{}, 10); len(queue.Entries) != 0 {
		t.Fatalf("queue after merge = %+v", queue)
	}
}

// One proposal at a stale version refuses the whole merge: the tables are
// byte-identical. A failure of the last write rolls back the earlier ones.
func TestMergeRefusalAndFailureWriteNothing(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	ids := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "Първо"), reportEntry("fact", "Второ")), "attempt-1", recordsNow))
	before := dumpMemoryTables(t, store)
	stale := mergeBatch("Обединено", records.MergedProposal{RecordID: ids[0], ExpectedVersion: 1}, records.MergedProposal{RecordID: ids[1], ExpectedVersion: 2})
	got, err := proposalService(store, nil).ReviewBatch(ctx, stale)
	if err != nil || got.Results[0].Applied || got.Results[0].Merge.Transitions[1].Decision.Kind != memory.TransitionVersionConflict {
		t.Fatalf("result %+v, error %v", got, err)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))

	failing := proposals.Service{Records: records.Service{Store: failingNthReview{Store: store.MemoryRecords(), fail: 3}}}
	valid := mergeBatch("Обединено", records.MergedProposal{RecordID: ids[0], ExpectedVersion: 1}, records.MergedProposal{RecordID: ids[1], ExpectedVersion: 1})
	got, err = failing.ReviewBatch(ctx, valid)
	if !errors.Is(err, ErrMemoryWriteDiverged) || got.Results != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
	assertPoolOutsideTransaction(t, store)
}

// failingNthReview makes the transaction's fail-th review name another
// content hash, so the engine's guard refuses it after the earlier writes.
type failingNthReview struct {
	records.Store
	fail int
}

func (f failingNthReview) WithinRecordWrite(ctx context.Context, operation func(records.Transaction) error) error {
	return f.Store.WithinRecordWrite(ctx, func(tx records.Transaction) error {
		return operation(&countingReviewTx{Transaction: tx, fail: f.fail})
	})
}

type countingReviewTx struct {
	records.Transaction
	fail, reviews int
}

func (c *countingReviewTx) InsertReview(ctx context.Context, review records.Review) error {
	c.reviews++
	if c.reviews == c.fail {
		review.ContentHash = memory.ContentHash("not this version")
	}
	return c.Transaction.InsertReview(ctx, review)
}

func composeInAttempt(t *testing.T, store *Store, files snapshotFiles, projectID, attemptID, query string) compose.Composition {
	t.Helper()
	composer, err := compose.New(store.MemoryReader(), files, redaction.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	got, err := composer.ComposeContext(context.Background(), compose.Request{ProjectID: projectID, AttemptID: attemptID, Snapshot: "snapshot", Query: query, Marker: marker})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A proposal recorded in one attempt and approved in a batch is eligible by
// its effective trust, and by its effective visibility in every attempt of
// the project (Martin, 2026-11-13), without a new version; it never reaches
// another project.
func TestApproveInABatchMakesTheProposalVisibleToTheProject(t *testing.T) {
	store := openMemoryStore(t)
	files := snapshotFiles{"internal/storage/turso/memory_write.go": "4bff34222a43b71631d0bc0a180aa03863cef75b"}
	mustRecordProposals(t, store, attemptReport(agentReport(
		reportEntry("gotcha", "Неуспешен COMMIT оставя транзакцията отворена", `{"kind":"file","path":"internal/storage/turso/memory_write.go","blob_hash":"4bff34222a43b71631d0bc0a180aa03863cef75b"}`)),
		"attempt-1", recordsNow))
	item := queuePage(t, store, records.ProposalPosition{}, 1).Entries[0].Item
	if kind := exclusionKind(composeInAttempt(t, store, files, "project", "attempt-1", "commit"), item.Record.ID); kind != compose.ExclusionKind(memory.RetrievalLifecycleExcluded) {
		t.Fatalf("proposal exclusion = %q", kind)
	}
	if kind := exclusionKind(composeInAttempt(t, store, files, "project", "attempt-2", "commit"), item.Record.ID); kind != compose.ExclusionKind(memory.RetrievalNotVisible) {
		t.Fatalf("unapproved proposal in another attempt exclusion = %q", kind)
	}
	got, err := proposalService(store, files).ReviewBatch(context.Background(), proposals.BatchRequest{
		ProjectID: "project", Actor: memory.ActorHuman, Reviewer: "martin", Now: recordsNow,
		Decisions: []proposals.Decision{
			{Kind: proposals.DecisionApprove, RecordID: item.Record.ID, ExpectedVersion: item.Record.Version, VersionID: item.Record.CurrentVersionID, ContentHash: item.ContentHash},
			{Kind: proposals.DecisionReject, RecordID: item.Record.ID, ExpectedVersion: item.Record.Version, Reason: "късно"},
		},
	})
	if err != nil || !got.Results[0].Applied || got.Results[1].Applied || got.Results[1].Transition.Kind != memory.TransitionVersionConflict {
		t.Fatalf("result %+v, error %v", got, err)
	}
	for _, attemptID := range []string{"attempt-1", "attempt-2", ""} {
		if trust, ok := itemTrust(composeInAttempt(t, store, files, "project", attemptID, "commit"), item.Record.ID); !ok || trust != memory.TrustHumanApproved {
			t.Fatalf("approved proposal is not eligible in attempt %q", attemptID)
		}
	}
	other := composeInAttempt(t, store, files, "other-project", "attempt-2", "commit")
	if _, ok := itemTrust(other, item.Record.ID); ok || exclusionKind(other, item.Record.ID) != "" {
		t.Fatalf("another project sees the proposal: %+v", other)
	}
	// Stored visibility is provenance: no new version, no rewrite.
	if scopes := queryStrings(t, store, "SELECT visibility || '|' || origin || '|' || attempt_id FROM memory_record_versions"); !slices.Equal(scopes, []string{"attempt|attempt|attempt-1"}) {
		t.Fatalf("stored scopes = %v", scopes)
	}
}

func TestReviewQueueReadFailures(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	ids := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "Неразчетимо")), "attempt-1", recordsNow))
	reader := store.ProposalQueue()
	if _, err := reader.ReadReviewQueue(ctx, "project", records.ProposalPosition{}, 0); err == nil {
		t.Fatal("zero page size accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := reader.ReadReviewQueue(cancelled, "project", records.ProposalPosition{}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read = %v", err)
	}
	mustExec(t, store, "UPDATE memory_records SET created_at = 'yesterday' WHERE id = ?", ids[0])
	if _, err := reader.ReadReviewQueue(ctx, "project", records.ProposalPosition{}, 1); !errors.Is(err, ErrInvalidStoredTimestamp) || strings.Contains(err.Error(), "yesterday") {
		t.Fatalf("invalid time = %v", err)
	}
	mustExec(t, store, "UPDATE memory_records SET created_at = ?, version = 'many' WHERE id = ?", formatStoredTime(recordsNow), ids[0])
	if _, err := reader.ReadReviewQueue(ctx, "project", records.ProposalPosition{}, 1); !errors.Is(err, ErrUndecodableMemoryRow) {
		t.Fatalf("undecodable row = %v", err)
	}
}

// Every statement of the queue read fails cleanly with its cause, and the
// pooled connection is not left inside the read transaction.
func TestReviewQueueReportsStatementFailures(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	seeded, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	mustRecordProposals(t, seeded, attemptReport(agentReport(reportEntry("fact", "Извор", `{"kind":"spec","ref":"specs/012"}`)), "attempt-1", recordsNow))
	if err := seeded.Close(); err != nil {
		t.Fatal(err)
	}
	db, connector := openFaulty(t, path, 5000)
	reader := memoryReader{db: db}
	read := func() error {
		page, err := reader.ReadReviewQueue(ctx, "project", records.ProposalPosition{}, 5)
		if err == nil && (len(page.Items) != 1 || len(page.Items[0].Sources) != 1) {
			return fmt.Errorf("page = %+v", page)
		}
		return err
	}
	if err := read(); err != nil {
		t.Fatal(err)
	}
	statements := slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(connector.statements))), func(statement string) bool {
		return strings.HasPrefix(statement, "PRAGMA")
	})
	if len(statements) < 2 {
		t.Fatalf("recorded statements = %q", statements)
	}
	for _, statement := range statements {
		injected := fmt.Errorf("%w: injected", tursogo.ErrTursoGeneric)
		connector.inject(strings.TrimSpace(statement), injected, 1)
		if err := read(); !errors.Is(err, injected) {
			t.Fatalf("failure of %.40q = %v", statement, err)
		}
		if err := read(); err != nil {
			t.Fatalf("read after failure of %.40q: %v", statement, err)
		}
	}
}

func TestSearchNormalizerMatchesTheStoredSearchText(t *testing.T) {
	store := openMemoryStore(t)
	mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "Памет ЗА Тест")), "attempt-1", recordsNow))
	stored := queuePage(t, store, records.ProposalPosition{}, 1).Entries[0].Item.SearchText
	normalized := store.SearchNormalizer().SearchTextIdentity("ПАМЕТ за тест")
	if !memory.SameSearchText(stored, normalized) || normalized.NormalizationVersion != searchNormalizationVersion {
		t.Fatalf("stored %+v, normalized %+v", stored, normalized)
	}
}

// Over-long agent values are rule refusals before any statement: an
// over-long path is that proposal's creation refusal, a blob hash outside
// its grammar drops the entry, the next one is recorded, and nothing fails
// the schema's CHECK.
func TestRecordProposalsRefusesOverLongAgentValues(t *testing.T) {
	store := openMemoryStore(t)
	longPath := fmt.Sprintf(`{"kind":"file","path":%q,"blob_hash":"0fd0bcfb44f83e7d5ac7a8922578276b9af48746"}`, strings.Repeat("п", memory.MaxTextFieldBytes/2)+"x")
	longHash := fmt.Sprintf(`{"kind":"file","path":"a.go","blob_hash":%q}`, strings.Repeat("a", memory.MaxIdentityBytes+1))
	report := agentReport(reportEntry("fact", "дълъг път", longPath), reportEntry("fact", "дълъг hash", longHash), reportEntry("fact", "нормално"))
	got, err := proposalService(store, nil).RecordProposals(context.Background(), attemptReport(report, "attempt-1", recordsNow))
	if err != nil || len(got.Recorded) != 2 || !slices.Equal(got.Extraction.Dropped, []proposals.DropCount{{Kind: memory.ProposalInvalidSource, Count: 1}}) {
		t.Fatalf("result %+v, error %v", got, err)
	}
	for index, want := range []memory.RecordCreationKind{memory.CreationInvalidSource, memory.CreationAllowed} {
		if got.Recorded[index].Result.Kind != want {
			t.Fatalf("recorded %d = %+v", index, got.Recorded[index])
		}
	}
	if contents := queryStrings(t, store, "SELECT content FROM memory_record_versions"); !slices.Equal(contents, []string{"нормално"}) {
		t.Fatalf("contents = %v", contents)
	}

	author := attemptReport(agentReport(reportEntry("fact", "автор")), "attempt-1", recordsNow)
	author.Author = strings.Repeat("a", memory.MaxIdentityBytes+1)
	got, err = proposalService(store, nil).RecordProposals(context.Background(), author)
	if err != nil || got.Recorded[0].Result.Kind != memory.CreationInvalidFacts {
		t.Fatalf("author result %+v, error %v", got, err)
	}
	attempt := attemptReport(agentReport(reportEntry("fact", "опит")), strings.Repeat("t", memory.MaxIdentityBytes+1), recordsNow)
	got, err = proposalService(store, nil).RecordProposals(context.Background(), attempt)
	if err != nil || got.Recorded[0].Result.Kind != memory.CreationInvalidScope {
		t.Fatalf("attempt result %+v, error %v", got, err)
	}
}

// A reason that redaction lengthens past the bound is refused by the rule;
// the tables are unchanged.
func TestReviewReasonsOverTheBoundAreRefused(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	ids := mustRecordProposals(t, store, attemptReport(agentReport(reportEntry("fact", "Предложение")), "attempt-1", recordsNow))
	reason := strings.Repeat("r", memory.MaxTextFieldBytes-12) + "\npassword=x"
	before := dumpMemoryTables(t, store)
	got, err := proposalService(store, nil).ReviewBatch(ctx, proposals.BatchRequest{
		ProjectID: "project", Actor: memory.ActorHuman, Reviewer: "martin", Now: recordsNow,
		Decisions: []proposals.Decision{{Kind: proposals.DecisionReject, RecordID: ids[0], ExpectedVersion: 1, Reason: reason}},
	})
	if err != nil || got.Results[0].Applied || got.Results[0].Transition.Kind != memory.TransitionReasonTooLong {
		t.Fatalf("result %+v, error %v", got, err)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
}

// A secret hidden by a JSON escape is masked in content, ref and path before
// it is stored or returned; as a blob hash it fails the grammar and the
// entry is dropped. No form of it reaches the tables or the result.
func TestHiddenSecretsNeverReachTheTablesOrTheResult(t *testing.T) {
	store := openMemoryStore(t)
	escaped := `p\u0061ssword=hunter2`
	report := agentReport(
		`{"category":"fact","content":"`+escaped+`","sources":[{"kind":"spec","ref":"`+escaped+`"},{"kind":"file","path":"`+escaped+`","blob_hash":"`+strings.Repeat("c", 64)+`"}]}`,
		`{"category":"fact","content":"друго","sources":[{"kind":"file","path":"a.go","blob_hash":"`+escaped+`"}]}`,
	)
	got, err := proposalService(store, nil).RecordProposals(context.Background(), attemptReport(report, "attempt-1", recordsNow))
	if err != nil || len(got.Recorded) != 1 || got.Recorded[0].Result.Kind != memory.CreationAllowed ||
		!slices.Equal(got.Extraction.Dropped, []proposals.DropCount{{Kind: memory.ProposalInvalidSource, Count: 1}}) {
		t.Fatalf("result %+v, error %v", got, err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "hunter2") || strings.Contains(fmt.Sprintf("%+v", got), "hunter2") {
		t.Fatalf("secret in result: %s", encoded)
	}
	for table, rows := range dumpMemoryTables(t, store) {
		for _, row := range rows {
			if strings.Contains(row, "hunter2") {
				t.Fatalf("secret stored in %s: %s", table, row)
			}
		}
	}
	if sources := queryStrings(t, store, "SELECT coalesce(ref, path) FROM memory_sources ORDER BY kind"); !slices.Equal(sources, []string{
		"password=[REDACTED:secret_assignment]", "password=[REDACTED:secret_assignment]",
	}) {
		t.Fatalf("sources = %v", sources)
	}
}
