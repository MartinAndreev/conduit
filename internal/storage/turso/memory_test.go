package turso

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/model"
	tursogo "turso.tech/database/tursogo"
)

var memoryEpoch = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// at is a version time minute minutes after the epoch, with a fixed-width
// fraction so text order is time order.
func at(minute int) string {
	return memoryEpoch.Add(time.Duration(minute) * time.Minute).Format("2006-01-02T15:04:05.000000000Z07:00")
}

// seedText inserts a current, human-origin record whose only version holds
// content, created at minute.
func seedText(t *testing.T, store *Store, project, id, content string, minute int) memoryFixture {
	t.Helper()
	fixture := newMemoryFixture(id, project)
	fixture.version.Content, fixture.version.SearchText = &content, optional(normalizeSearchText(content))
	fixture.version.CreatedAt = at(minute)
	mustInsertMemory(t, store.db, fixture)
	return fixture
}

func mustExec(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

// addCurrentVersion inserts a new version of fixture's record and makes it
// current.
func addCurrentVersion(t *testing.T, store *Store, fixture memoryFixture, id, content string, minute int) model.MemoryRecordVersions {
	t.Helper()
	version := fixture.version
	version.ID, version.ContentHash, version.CreatedAt = id, "sha256:"+id, at(minute)
	version.Content, version.SearchText = &content, optional(normalizeSearchText(content))
	if err := insertVersion(context.Background(), store.db, version); err != nil {
		t.Fatal(err)
	}
	mustExec(t, store, "UPDATE memory_records SET current_version_id = ?, version = version + 1 WHERE id = ?", id, fixture.record.ID)
	return version
}

func mustReview(t *testing.T, store *Store, review model.MemoryReviews) {
	t.Helper()
	if err := insertReview(context.Background(), store.db, review); err != nil {
		t.Fatal(err)
	}
}

func relate(t *testing.T, store *Store, project, from, to, kind string) {
	t.Helper()
	mustExec(t, store, "INSERT INTO memory_relations (project_id, from_record_id, to_record_id, kind, created_at) VALUES (?, ?, ?, ?, ?)", project, from, to, kind, memoryTime)
}

func linkedIDs(candidates []compose.Candidate) []string {
	ids := make([]string, len(candidates))
	for index, candidate := range candidates {
		ids[index] = candidate.Record.ID
	}
	return ids
}

func hitSummary(result compose.SearchResult) []string {
	summary := make([]string, len(result.Hits))
	for index, hit := range result.Hits {
		summary[index] = fmt.Sprintf("%s:%d", hit.Version.ID, hit.MatchedTerms)
	}
	return summary
}

func search(t *testing.T, store *Store, project, text string, limit int) compose.SearchResult {
	t.Helper()
	result, err := store.MemoryReader().SearchRecords(context.Background(), compose.SearchQuery{ProjectID: project, Text: text, ScanLimit: limit})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestReadLinkedRecordsReturnsCurrentVersionFacts(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	subject := newMemoryFixture("subject", "project")
	subject.record.Status = string(memory.StatusNeedsVerification)
	subject.version.Origin, subject.version.Visibility = string(memory.OriginAttempt), string(memory.VisibilityProject)
	subject.version.RunID, subject.version.AttemptID, subject.version.BaseCommit = optional("run"), optional("attempt"), optional("abc123")
	subject.version.AcceptedResultRef, subject.version.Trust = optional("result"), string(memory.TrustConduitObserved)
	subject.version.CreatedAt = at(1)
	mustInsertMemory(t, store.db, subject)
	current := addCurrentVersion(t, store, subject, "subject-v2", "Второ решение", 2)
	for _, source := range []model.MemorySources{
		{ID: "s2", VersionID: current.ID, Kind: string(memory.SourceSpec), Ref: optional("specs/012")},
		{ID: "s1", VersionID: current.ID, Kind: string(memory.SourceFile), Path: optional("internal/memory/types.go"), BlobHash: optional("blob")},
		{ID: "s0", VersionID: subject.version.ID, Kind: string(memory.SourceEvent), Ref: optional("old-version-only")},
	} {
		if err := insertSource(ctx, store.db, source); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"rival", "critic", "successor", "plain"} {
		seedText(t, store, "project", id, "текст", 0)
	}
	mustExec(t, store, "UPDATE memory_records SET status = ? WHERE id = ?", string(memory.StatusArchived), "critic")
	relate(t, store, "project", "subject", "rival", relationContradicts)
	relate(t, store, "project", "rival", "subject", relationContradicts)
	relate(t, store, "project", "critic", "subject", relationContradicts)
	relate(t, store, "project", "successor", "subject", "supersedes")
	seedText(t, store, "other", "foreign", "текст", 0)

	got, err := store.MemoryReader().ReadLinkedRecords(ctx, "project", []string{"plain", "missing", "subject", "foreign", "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := linkedIDs(got); !slices.Equal(ids, []string{"plain", "subject"}) {
		t.Fatalf("linked ids = %v", ids)
	}
	c := got[1]
	wantVersion := memory.RecordVersion{
		ID: "subject-v2", RecordID: "subject", Content: "Второ решение", ContentHash: "sha256:subject-v2",
		Trust: memory.TrustConduitObserved,
		Scope: memory.Scope{ProjectID: "project", Visibility: memory.VisibilityProject, Origin: memory.OriginAttempt, RunID: "run", AttemptID: "attempt", BaseCommit: "abc123", AcceptedResultRef: "result"},
	}
	wantRecord := memory.Record{ID: "subject", ProjectID: "project", Status: memory.StatusNeedsVerification, CurrentVersionID: "subject-v2", Version: 2, CreatedAt: memoryEpoch}
	if !c.RecordFound || c.Record != wantRecord || c.Version != wantVersion || c.ContentDeleted || c.ExactVersionApproved {
		t.Fatalf("candidate = %+v", c)
	}
	if !c.VersionCreatedAt.Equal(memoryEpoch.Add(2 * time.Minute)) {
		t.Fatalf("version time = %v", c.VersionCreatedAt)
	}
	wantSources := []memory.RecordSource{
		{Kind: memory.SourceFile, Path: "internal/memory/types.go", BlobHash: "blob"},
		{Kind: memory.SourceSpec, Ref: "specs/012"},
	}
	if !slices.Equal(c.Sources, wantSources) {
		t.Fatalf("sources = %+v", c.Sources)
	}
	wantContradicts := []compose.RelatedRecord{{RecordID: "critic", Status: memory.StatusArchived}, {RecordID: "rival", Status: memory.StatusActive}}
	if !slices.Equal(c.Contradicts, wantContradicts) {
		t.Fatalf("contradicts = %+v", c.Contradicts)
	}
	if plain := got[0]; len(plain.Sources) != 0 || len(plain.Contradicts) != 0 {
		t.Fatalf("plain record facts = %+v", plain)
	}
}

func TestReadLinkedRecordsWithoutIDsOrMatches(t *testing.T) {
	store := openMemoryStore(t)
	for _, ids := range [][]string{nil, {"missing"}} {
		got, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", ids)
		if err != nil || len(got) != 0 {
			t.Fatalf("ids %v: %v, %v", ids, got, err)
		}
	}
}

// Only a human approval of the exact current version and its content hash
// counts; opinions, rejections and approvals of other versions do not.
func TestExactVersionApprovalNeedsHumanApprovalOfThatVersion(t *testing.T) {
	store := openMemoryStore(t)
	cases := map[string]bool{"approved": true, "agent-opinion": false, "system-opinion": false, "human-opinion": false, "rejected": false, "approved-earlier": false, "unreviewed": false}
	for id := range cases {
		fixture := seedText(t, store, "project", id, "текст", 0)
		switch id {
		case "approved":
			mustReview(t, store, newReview(id, fixture.version, memory.ActorHuman, memory.ReviewApprove))
		case "agent-opinion":
			mustReview(t, store, newReview(id, fixture.version, memory.ActorAgent, memory.ReviewOpinion))
		case "system-opinion":
			mustReview(t, store, newReview(id, fixture.version, memory.ActorSystem, memory.ReviewOpinion))
		case "human-opinion":
			mustReview(t, store, newReview(id, fixture.version, memory.ActorHuman, memory.ReviewOpinion))
		case "rejected":
			mustReview(t, store, newReview(id, fixture.version, memory.ActorHuman, memory.ReviewReject))
		case "approved-earlier":
			mustReview(t, store, newReview(id, fixture.version, memory.ActorHuman, memory.ReviewApprove))
			addCurrentVersion(t, store, fixture, id+"-v2", "текст", 1)
		}
	}
	got, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", []string{"approved", "agent-opinion", "system-opinion", "human-opinion", "rejected", "approved-earlier", "unreviewed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(cases) {
		t.Fatalf("got %d candidates", len(got))
	}
	for _, candidate := range got {
		if candidate.ExactVersionApproved != cases[candidate.Record.ID] {
			t.Errorf("%s approved = %v", candidate.Record.ID, candidate.ExactVersionApproved)
		}
	}
	result := search(t, store, "project", "текст", 10)
	for _, hit := range result.Hits {
		if hit.ExactVersionApproved != cases[hit.Record.ID] {
			t.Errorf("search %s approved = %v", hit.Record.ID, hit.ExactVersionApproved)
		}
	}
}

func TestContentDeletedVersionIsReadButNotSearched(t *testing.T) {
	store := openMemoryStore(t)
	fixture := seedText(t, store, "project", "deleted", "изтрита памет", 0)
	mustExec(t, store, "UPDATE memory_record_versions SET content = NULL, search_text = NULL, content_deleted_at = ? WHERE id = ?", memoryTime, fixture.version.ID)
	got, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", []string{"deleted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].ContentDeleted || got[0].Version.Content != "" || got[0].Version.ID != fixture.version.ID {
		t.Fatalf("deleted candidate = %+v", got)
	}
	if result := search(t, store, "project", "памет", 1); len(result.Hits) != 0 || result.LimitReached {
		t.Fatalf("deleted content searched: %+v", result)
	}
}

// Linked IDs beyond one IN list are read in chunks.
func TestReadLinkedRecordsChunksIDs(t *testing.T) {
	store := openMemoryStore(t)
	var ids, want []string
	for index := range 2*memoryIDChunk + 10 {
		id := fmt.Sprintf("record-%04d", index)
		ids = append(ids, id)
		if index%2 == 0 {
			seedText(t, store, "project", id, "текст", index)
			want = append(want, id)
		}
	}
	got, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", ids)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(linkedIDs(got), want) {
		t.Fatalf("got %d of %d records", len(got), len(want))
	}
	result := search(t, store, "project", "текст", len(want))
	if len(result.Hits) != len(want) || result.LimitReached {
		t.Fatalf("search over chunks: %d hits, limit %v", len(result.Hits), result.LimitReached)
	}
}

func TestSearchStaysInProjectAndFoldsUnicodeCase(t *testing.T) {
	store := openMemoryStore(t)
	seedText(t, store, "project", "here", "Проектната памет се пази локално", 0)
	seedText(t, store, "other", "there", "Проектната памет се пази локално", 1)
	result := search(t, store, "project", "ПАМЕТ", 10)
	if result.QueryTerms != 1 || !slices.Equal(hitSummary(result), []string{"here-v1:1"}) || result.LimitReached {
		t.Fatalf("result = %+v", result)
	}
	if result.Hits[0].Record.ProjectID != "project" || !result.Hits[0].RecordFound {
		t.Fatalf("hit = %+v", result.Hits[0])
	}
	if result := search(t, store, "missing-project", "памет", 10); len(result.Hits) != 0 {
		t.Fatalf("unknown project hits = %+v", result.Hits)
	}
}

// Turso's LIKE honours ESCAPE '\', so %, _ and \ in terms match literally.
func TestSearchTermsMatchWildcardsLiterally(t *testing.T) {
	store := openMemoryStore(t)
	for minute, content := range []string{"500 записа", "50% от записите", "axb", "a_b", `c:\dir`, "c:dir"} {
		seedText(t, store, "project", fmt.Sprintf("r%d", minute), content, minute)
	}
	for query, want := range map[string][]string{
		"50%":     {"r1-v1:1"},
		"a_b":     {"r3-v1:1"},
		`c:\d`:    {"r4-v1:1"},
		"%":       {"r1-v1:1"},
		"_":       {"r3-v1:1"},
		"50% a_b": {"r3-v1:1", "r1-v1:1"},
	} {
		if got := hitSummary(search(t, store, "project", query, 10)); !slices.Equal(got, want) {
			t.Errorf("query %q hits = %v, want %v", query, got, want)
		}
	}
}

func TestSearchCountsDistinctMatchedTerms(t *testing.T) {
	store := openMemoryStore(t)
	seedText(t, store, "project", "both", "alpha и beta", 0)
	seedText(t, store, "project", "one", "само gamma", 1)
	seedText(t, store, "project", "none", "нищо", 2)
	seedText(t, store, "project", "repeated", "alpha alpha alphabet", 3)
	result := search(t, store, "project", "Alpha  BETA gamma alpha", 10)
	if result.QueryTerms != 3 {
		t.Fatalf("query terms = %d", result.QueryTerms)
	}
	if got := hitSummary(result); !slices.Equal(got, []string{"repeated-v1:1", "one-v1:1", "both-v1:2"}) {
		t.Fatalf("hits = %v", got)
	}
}

func TestSearchTermLimitAndBlankQuery(t *testing.T) {
	store := openMemoryStore(t)
	seedText(t, store, "project", "late", "term16", 0)
	seedText(t, store, "project", "early", "term00", 1)
	var terms []string
	for index := range maxSearchTerms + 4 {
		terms = append(terms, fmt.Sprintf("term%02d", index))
	}
	result := search(t, store, "project", strings.Join(terms, " "), 10)
	if result.QueryTerms != maxSearchTerms || !slices.Equal(hitSummary(result), []string{"early-v1:1"}) {
		t.Fatalf("capped result = %+v", hitSummary(result))
	}
	for _, blank := range []string{"", " \t\n "} {
		result := search(t, store, "project", blank, 10)
		if result.QueryTerms != 0 || len(result.Hits) != 0 || result.LimitReached {
			t.Fatalf("blank %q = %+v", blank, result)
		}
	}
	for _, limit := range []int{0, -1} {
		if _, err := store.MemoryReader().SearchRecords(context.Background(), compose.SearchQuery{ProjectID: "project", Text: "term00", ScanLimit: limit}); err == nil {
			t.Fatalf("scan limit %d accepted", limit)
		}
	}
}

// The window is the newest ScanLimit current, non-deleted versions, matching
// or not; ties on created_at order by version id, descending.
func TestSearchScanLimitCountsScannedRows(t *testing.T) {
	store := openMemoryStore(t)
	seedText(t, store, "project", "m1", "памет едно", 1)
	seedText(t, store, "project", "m2", "памет две", 2)
	seedText(t, store, "project", "m3a", "памет три", 3)
	seedText(t, store, "project", "m3b", "памет три", 3)
	seedText(t, store, "project", "other", "друго", 4)
	deleted := seedText(t, store, "project", "deleted", "памет", 5)
	mustExec(t, store, "UPDATE memory_record_versions SET content = NULL, search_text = NULL, content_deleted_at = ? WHERE id = ?", memoryTime, deleted.version.ID)
	for _, tc := range []struct {
		limit   int
		hits    []string
		reached bool
	}{
		{1, nil, true},
		{2, []string{"m3b-v1:1"}, true},
		{3, []string{"m3b-v1:1", "m3a-v1:1"}, true},
		{5, []string{"m3b-v1:1", "m3a-v1:1", "m2-v1:1", "m1-v1:1"}, false},
		{6, []string{"m3b-v1:1", "m3a-v1:1", "m2-v1:1", "m1-v1:1"}, false},
	} {
		result := search(t, store, "project", "памет", tc.limit)
		if got := hitSummary(result); !slices.Equal(got, tc.hits) || result.LimitReached != tc.reached {
			t.Errorf("limit %d: hits %v reached %v", tc.limit, got, result.LimitReached)
		}
	}
}

func TestSearchScansOnlyCurrentVersions(t *testing.T) {
	store := openMemoryStore(t)
	fixture := seedText(t, store, "project", "edited", "стар термин", 1)
	addCurrentVersion(t, store, fixture, "edited-v2", "нов текст", 2)
	if result := search(t, store, "project", "стар", 10); len(result.Hits) != 0 {
		t.Fatalf("non-current version matched: %v", hitSummary(result))
	}
	result := search(t, store, "project", "нов", 1)
	if got := hitSummary(result); !slices.Equal(got, []string{"edited-v2:1"}) || result.LimitReached {
		t.Fatalf("current version: %v reached %v", got, result.LimitReached)
	}
}

// Search text from another normalization version cannot be compared: inside
// the window it fails the search, outside it is never read.
func TestSearchRefusesOtherNormalizationVersionInWindow(t *testing.T) {
	store := openMemoryStore(t)
	stale := seedText(t, store, "project", "stale", "друг текст", 1)
	mustExec(t, store, "UPDATE memory_record_versions SET search_normalization_version = ? WHERE id = ?", searchNormalizationVersion+1, stale.version.ID)
	seedText(t, store, "project", "fresh", "памет", 2)
	if result := search(t, store, "project", "памет", 1); !slices.Equal(hitSummary(result), []string{"fresh-v1:1"}) || !result.LimitReached {
		t.Fatalf("outside window: %+v", result)
	}
	_, err := store.MemoryReader().SearchRecords(context.Background(), compose.SearchQuery{ProjectID: "project", Text: "памет", ScanLimit: 2})
	if !errors.Is(err, ErrSearchNormalizationMismatch) {
		t.Fatalf("inside window error = %v", err)
	}
}

func TestMemoryReadsHonourCancellation(t *testing.T) {
	store := openMemoryStore(t)
	seedText(t, store, "project", "record", "памет", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := store.MemoryReader()
	if _, err := reader.ReadLinkedRecords(ctx, "project", []string{"record"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("linked read error = %v", err)
	}
	if _, err := reader.SearchRecords(ctx, compose.SearchQuery{ProjectID: "project", Text: "памет", ScanLimit: 10}); !errors.Is(err, context.Canceled) {
		t.Fatalf("search error = %v", err)
	}
	if _, err := reader.ReadLinkedRecords(context.Background(), "project", []string{"record"}); err != nil {
		t.Fatalf("read after cancellation: %v", err)
	}
}

// A stored time that does not parse is a technical failure, not a fact.
func TestMemoryReadsRejectUnparsableTimes(t *testing.T) {
	store := openMemoryStore(t)
	for _, column := range []string{"memory_records", "memory_record_versions"} {
		fixture := seedText(t, store, "project", column, "памет", 0)
		id := fixture.record.ID
		if column == "memory_record_versions" {
			id = fixture.version.ID
		}
		mustExec(t, store, "UPDATE "+column+" SET created_at = 'yesterday' WHERE id = ?", id)
		if _, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", []string{fixture.record.ID}); err == nil {
			t.Fatalf("%s time accepted", column)
		}
		if _, err := store.MemoryReader().SearchRecords(context.Background(), compose.SearchQuery{ProjectID: "project", Text: "памет", ScanLimit: 10}); err == nil {
			t.Fatalf("%s time accepted by search", column)
		}
		mustExec(t, store, "UPDATE "+column+" SET created_at = ? WHERE id = ?", memoryTime, id)
	}
	if _, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", []string{"memory_records", "memory_record_versions"}); err != nil {
		t.Fatal(err)
	}
}

// Every statement of both reads fails cleanly: the error keeps its cause and
// the pooled connection is not left inside the read transaction.
func TestMemoryReadsReportStatementFailures(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	seeded := &Store{db: store.db}
	fixture := seedText(t, seeded, "project", "subject", "памет", 0)
	seedText(t, seeded, "project", "rival", "друго", 1)
	relate(t, seeded, "project", "subject", "rival", relationContradicts)
	if err := insertSource(ctx, store.db, model.MemorySources{ID: "s", VersionID: fixture.version.ID, Kind: string(memory.SourceSpec), Ref: optional("spec")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, connector := openFaulty(t, path, 5000)
	reader := memoryReader{db: db}
	read := func() error {
		if _, err := reader.ReadLinkedRecords(ctx, "project", []string{"subject"}); err != nil {
			return err
		}
		_, err := reader.SearchRecords(ctx, compose.SearchQuery{ProjectID: "project", Text: "памет", ScanLimit: 10})
		return err
	}
	if err := read(); err != nil {
		t.Fatal(err)
	}
	statements := slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(connector.statements))), func(statement string) bool {
		return strings.HasPrefix(statement, "PRAGMA")
	})
	if len(statements) < 4 {
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

// Turso's length() of a BLOB counts bytes: Cyrillic letters take two.
func TestReaderReportsStoredSizesInBytes(t *testing.T) {
	store := openMemoryStore(t)
	fixture := seedText(t, store, "project", "sized", "памет", 0)
	for _, source := range []model.MemorySources{
		{ID: "s1", VersionID: fixture.version.ID, Kind: string(memory.SourceSpec), Ref: optional("спец")},
		{ID: "s2", VersionID: fixture.version.ID, Kind: string(memory.SourceFile), Path: optional("a/b.go"), BlobHash: optional("blob")},
	} {
		if err := insertSource(context.Background(), store.db, source); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", []string{"sized"})
	if err != nil {
		t.Fatal(err)
	}
	c := got[0]
	if c.ContentBytes != 10 || c.Version.Content != "памет" || c.SourceCount != 2 || c.SourceBytes != 18 || memory.SourceBytes(c.Sources) != 18 {
		t.Fatalf("sizes = content %d %q, sources %d/%d %+v", c.ContentBytes, c.Version.Content, c.SourceCount, c.SourceBytes, c.Sources)
	}
}

// Content above memory.MaxMaterialBytes and sources above the source limits
// are reported by size and not loaded, for linked reads and search alike.
func TestReaderWithholdsOversizedMaterial(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	atLimit := strings.Repeat("п", memory.MaxMaterialBytes/2)
	seedText(t, store, "project", "at-limit", atLimit, 0)
	seedText(t, store, "project", "over-limit", atLimit+"п", 1)
	hugeRef := seedText(t, store, "project", "huge-ref", "памет", 2)
	// Each ref is within the schema's bound; together they exceed MaxSourceBytes.
	for index := range 3 {
		if err := insertSource(ctx, store.db, model.MemorySources{ID: fmt.Sprintf("huge-%d", index), VersionID: hugeRef.version.ID, Kind: string(memory.SourceEvent), Ref: optional(strings.Repeat("р", 1500))}); err != nil {
			t.Fatal(err)
		}
	}
	many := seedText(t, store, "project", "many", "памет", 3)
	full := seedText(t, store, "project", "full", "памет", 4)
	for index := range memory.MaxSourcesPerItem + 1 {
		for _, version := range []model.MemoryRecordVersions{many.version, full.version} {
			if version.ID == full.version.ID && index == memory.MaxSourcesPerItem {
				continue
			}
			source := model.MemorySources{ID: fmt.Sprintf("%s-%02d", version.ID, index), VersionID: version.ID, Kind: string(memory.SourceSpec), Ref: optional(fmt.Sprintf("spec-%02d", index))}
			if err := insertSource(ctx, store.db, source); err != nil {
				t.Fatal(err)
			}
		}
	}
	type sizes struct {
		contentBytes, loadedContent, sourceCount, loadedSources int64
	}
	want := map[string]sizes{
		"at-limit":   {memory.MaxMaterialBytes, memory.MaxMaterialBytes, 0, 0},
		"over-limit": {memory.MaxMaterialBytes + 2, 0, 0, 0},
		"huge-ref":   {10, 10, 3, 0},
		"many":       {10, 10, memory.MaxSourcesPerItem + 1, 0},
		"full":       {10, 10, memory.MaxSourcesPerItem, memory.MaxSourcesPerItem},
	}
	check := func(how string, candidates []compose.Candidate) {
		t.Helper()
		if len(candidates) != len(want) {
			t.Fatalf("%s: %d candidates", how, len(candidates))
		}
		for _, c := range candidates {
			got := sizes{c.ContentBytes, int64(len(c.Version.Content)), c.SourceCount, int64(len(c.Sources))}
			if got != want[c.Record.ID] {
				t.Errorf("%s %s: sizes %+v, want %+v", how, c.Record.ID, got, want[c.Record.ID])
			}
		}
		for _, c := range candidates {
			if c.Record.ID == "huge-ref" && c.SourceBytes != 9000 {
				t.Errorf("%s huge ref bytes = %d", how, c.SourceBytes)
			}
		}
	}
	linked, err := store.MemoryReader().ReadLinkedRecords(ctx, "project", []string{"at-limit", "over-limit", "huge-ref", "many", "full"})
	if err != nil {
		t.Fatal(err)
	}
	check("linked", linked)
	result := search(t, store, "project", "п", 10)
	var hits []compose.Candidate
	for _, hit := range result.Hits {
		hits = append(hits, hit.Candidate)
	}
	check("search", hits)
}

// A very long query is read only up to maxSearchQueryBytes and stops at
// maxSearchTerms distinct terms; a term cut by the byte bound is dropped.
func TestSearchTermsBoundLongQueries(t *testing.T) {
	repeated := strings.Repeat("памет ", 1_000_000)
	if got := searchTerms(repeated); !slices.Equal(got, []string{"памет"}) {
		t.Fatalf("repeated query terms = %v", got)
	}
	var many []string
	for index := range 10_000 {
		many = append(many, fmt.Sprintf("T%05d", index))
	}
	if got := searchTerms(strings.Join(many, " ")); len(got) != maxSearchTerms || got[0] != "t00000" || got[maxSearchTerms-1] != "t00015" {
		t.Fatalf("many terms = %v", got)
	}
	cut := strings.Repeat("a", maxSearchQueryBytes-3) + " ПАМЕТ"
	if got := searchTerms(cut); !slices.Equal(got, []string{strings.Repeat("a", maxSearchQueryBytes-3)}) {
		t.Fatalf("cut term kept: %d terms, last %q", len(got), got[len(got)-1][:min(8, len(got[len(got)-1]))])
	}
	exact := strings.Repeat("a", maxSearchQueryBytes) + " памет"
	if got := searchTerms(exact); len(got) != 1 || len(got[0]) != maxSearchQueryBytes {
		t.Fatalf("term ending at the bound: %d terms", len(got))
	}
	if got := searchTerms(strings.Repeat("я", maxSearchQueryBytes)); len(got) != 0 {
		t.Fatalf("single over-long term kept: %d terms", len(got))
	}
	store := openMemoryStore(t)
	seedText(t, store, "project", "record", "памет", 0)
	result := search(t, store, "project", repeated, 10)
	if result.QueryTerms != 1 || !slices.Equal(hitSummary(result), []string{"record-v1:1"}) {
		t.Fatalf("long query result = %+v", hitSummary(result))
	}
}

// Contradicts relations are read up to memory.MaxContradictsPerItem distinct
// records per item, in record id order; a record with more is marked
// truncated. A pair related in both directions counts once.
func TestReaderCapsContradictsPerRecord(t *testing.T) {
	store := openMemoryStore(t)
	seedText(t, store, "project", "crowded", "памет", 0)
	seedText(t, store, "project", "exact", "памет", 1)
	limit := memory.MaxContradictsPerItem
	for index := range limit + 6 {
		other := fmt.Sprintf("other-%03d", index)
		seedText(t, store, "project", other, "друго", 2)
		switch {
		case index < limit/2:
			relate(t, store, "project", "crowded", other, relationContradicts)
		case index < limit/2+4:
			relate(t, store, "project", "crowded", other, relationContradicts)
			relate(t, store, "project", other, "crowded", relationContradicts)
		default:
			relate(t, store, "project", other, "crowded", relationContradicts)
		}
		if index < limit {
			relate(t, store, "project", "exact", other, relationContradicts)
			if index%3 == 0 {
				relate(t, store, "project", other, "exact", relationContradicts)
			}
		}
	}
	check := func(how string, candidates []compose.Candidate) {
		t.Helper()
		for _, c := range candidates {
			if c.Record.ID != "crowded" && c.Record.ID != "exact" {
				continue
			}
			if len(c.Contradicts) != limit || c.Contradicts[0].RecordID != "other-000" || c.Contradicts[limit-1].RecordID != fmt.Sprintf("other-%03d", limit-1) {
				t.Errorf("%s %s: %d contradicts", how, c.Record.ID, len(c.Contradicts))
			}
			if c.ContradictsTruncated != (c.Record.ID == "crowded") {
				t.Errorf("%s %s: truncated = %v", how, c.Record.ID, c.ContradictsTruncated)
			}
		}
	}
	linked, err := store.MemoryReader().ReadLinkedRecords(context.Background(), "project", []string{"crowded", "exact"})
	if err != nil || len(linked) != 2 {
		t.Fatalf("linked = %d, %v", len(linked), err)
	}
	check("linked", linked)
	result := search(t, store, "project", "памет", 100)
	var hits []compose.Candidate
	for _, hit := range result.Hits {
		hits = append(hits, hit.Candidate)
	}
	if len(hits) != 2 {
		t.Fatalf("search hits = %v", hitSummary(result))
	}
	check("search", hits)
}
