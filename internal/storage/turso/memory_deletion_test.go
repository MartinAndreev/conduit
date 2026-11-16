package turso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/model"
)

// leakMarker is in every text the deletion must remove and nowhere else.
const leakMarker = "zzleak"

var deletionNow = time.Date(2026, 11, 11, 10, 0, 0, 5, time.FixedZone("EET", 2*60*60))

var doomedVersions = []string{"doomed-v1", "doomed-v2", "doomed-v3"}

// seedDoomed stores record "doomed" with three versions (current v3, record
// version 3), each with a spec and a file source and a review with a reason:
// a human approval of v3 and a human dismissal of an archive suggestion on
// v3, a human rejection of v1, an agent and a system opinion on v2. Every deleted text carries leakMarker. Record "keeper" stays,
// with its own source and review reason, and contradicts "doomed".
func seedDoomed(t *testing.T, store *Store) map[string]string {
	t.Helper()
	ctx := context.Background()
	fixture := newMemoryFixture("doomed", "project")
	fixture.record.CurrentVersionID, fixture.record.Version = "doomed-v3", 3
	hashes := make(map[string]string)
	var versions []model.MemoryRecordVersions
	for index, id := range doomedVersions {
		content := fmt.Sprintf("Решение v%d: ПАМЕТТА %s", index+1, leakMarker)
		sum := sha256.Sum256([]byte(content))
		version := fixture.version
		version.ID, version.ContentHash, version.CreatedAt = id, "sha256:"+hex.EncodeToString(sum[:]), at(index)
		version.Content, version.SearchText = &content, optional(normalizeSearchText(content))
		hashes[id] = version.ContentHash
		versions = append(versions, version)
	}
	err := inTransaction(ctx, store.db, func(tx database) error {
		if err := insertRecord(ctx, tx, fixture.record); err != nil {
			return err
		}
		for _, version := range versions {
			if err := insertVersion(ctx, tx, version); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range versions {
		for _, source := range []model.MemorySources{
			{ID: version.ID + "-spec", VersionID: version.ID, Kind: string(memory.SourceSpec), Ref: optional("specs/" + leakMarker + "/spec.md")},
			{ID: version.ID + "-file", VersionID: version.ID, Kind: string(memory.SourceFile), Path: optional("internal/" + leakMarker + ".go"), BlobHash: optional("blob")},
		} {
			if err := insertSource(ctx, store.db, source); err != nil {
				t.Fatal(err)
			}
		}
	}
	reviews := []model.MemoryReviews{
		newReview("approve-v3", versions[2], memory.ActorHuman, memory.ReviewApprove),
		newReview("reject-v1", versions[0], memory.ActorHuman, memory.ReviewReject),
		newReview("agent-v2", versions[1], memory.ActorAgent, memory.ReviewOpinion),
		newReview("system-v2", versions[1], memory.ActorSystem, memory.ReviewOpinion),
		newReview("dismiss-v3", versions[2], memory.ActorHuman, memory.ReviewDismiss),
	}
	reviews[len(reviews)-1].Suggestion = optional(string(memory.SuggestArchive))
	for _, review := range reviews {
		review.Reason = optional("причина " + leakMarker + " от " + review.ID)
		mustReview(t, store, review)
	}
	keeper := seedText(t, store, "project", "keeper", "Друго решение за паметта", 4)
	if err := insertSource(ctx, store.db, model.MemorySources{ID: "keeper-spec", VersionID: keeper.version.ID, Kind: string(memory.SourceSpec), Ref: optional("specs/keeper")}); err != nil {
		t.Fatal(err)
	}
	keeperReview := newReview("keeper-reject", keeper.version, memory.ActorHuman, memory.ReviewReject)
	keeperReview.Reason = optional("остава")
	mustReview(t, store, keeperReview)
	relate(t, store, "project", "keeper", "doomed", "contradicts")
	return hashes
}

func deleteDoomed(t *testing.T, store *Store, mutate func(*deletion.Request)) deletion.Result {
	t.Helper()
	request := deletion.Request{ProjectID: "project", RecordID: "doomed", ExpectedVersion: 3, Actor: memory.ActorHuman, Now: deletionNow}
	if mutate != nil {
		mutate(&request)
	}
	result, err := deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// memoryTables lists every memory_* and context_* table.
func memoryTables(t *testing.T, store *Store) []string {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), `SELECT name FROM sqlite_schema WHERE type = 'table' AND (name LIKE 'memory\_%' ESCAPE '\' OR name LIKE 'context\_%' ESCAPE '\') ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"context_deletion_receipts", "context_manifest_exclusions", "context_manifest_items", "context_manifest_payloads", "context_manifests", "memory_record_versions", "memory_records", "memory_relations", "memory_reviews", "memory_sources"}
	if !slices.Equal(names, want) {
		t.Fatalf("memory tables = %v", names)
	}
	return names
}

func tableColumns(t *testing.T, store *Store, name string) []string {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), "PRAGMA table_info("+name+")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columnNames, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for rows.Next() {
		values := make([]any, len(columnNames))
		pointers := make([]any, len(values))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, fmt.Sprint(values[slices.Index(columnNames, "name")]))
	}
	if err := rows.Err(); err != nil || len(columns) == 0 {
		t.Fatalf("columns of %s: %v, %v", name, columns, err)
	}
	return columns
}

// dumpMemoryTables renders every row of every memory_* and context_* table
// as text, each column cast to TEXT, sorted per table.
func dumpMemoryTables(t *testing.T, store *Store) map[string][]string {
	t.Helper()
	dump := make(map[string][]string)
	for _, name := range memoryTables(t, store) {
		columns := tableColumns(t, store, name)
		projections := make([]string, len(columns))
		for index, column := range columns {
			projections[index] = "CAST(" + column + " AS TEXT)"
		}
		rows, err := store.db.QueryContext(context.Background(), "SELECT "+strings.Join(projections, ", ")+" FROM "+name)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]*string, len(columns))
			pointers := make([]any, len(values))
			for index := range values {
				pointers[index] = &values[index]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			cells := make([]string, len(values))
			for index, value := range values {
				cells[index] = columns[index] + "=<NULL>"
				if value != nil {
					cells[index] = columns[index] + "=" + *value
				}
			}
			dump[name] = append(dump[name], strings.Join(cells, " | "))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(dump[name])
	}
	return dump
}

func assertUnchanged(t *testing.T, before, after map[string][]string) {
	t.Helper()
	for name, rows := range before {
		if !slices.Equal(rows, after[name]) {
			t.Fatalf("%s changed:\nbefore %v\nafter  %v", name, rows, after[name])
		}
	}
}

func queryStrings(t *testing.T, store *Store, query string, args ...any) []string {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func TestDeleteRecordClearsContentOfEveryVersion(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	hashes := seedDoomed(t, store)
	composer, err := compose.New(store.MemoryReader(), noFiles{}, redaction.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	before, err := composer.ComposeContext(ctx, compose.Request{ProjectID: "project", Snapshot: "snapshot", ExplicitRecordIDs: []string{"doomed"}, Marker: marker})
	if err != nil || before.Kind != compose.Composed || !strings.Contains(before.Part.Text, leakMarker) {
		t.Fatalf("composition before deletion: %s %s %v", before.Kind, before.Reason, err)
	}
	if hits := hitSummary(search(t, store, "project", leakMarker, 100)); len(hits) != 1 {
		t.Fatalf("search before deletion = %v", hits)
	}
	dumpBefore := dumpMemoryTables(t, store)

	result := deleteDoomed(t, store, nil)
	if result.Kind != memory.DeletionDelete || result.RecordVersion != 4 || len(result.Receipts) != 3 {
		t.Fatalf("result = %+v", result)
	}

	deletedAt := formatStoredTime(deletionNow)
	if deletedAt != "2026-11-11T08:00:00.000000005Z" {
		t.Fatalf("deleted at = %s", deletedAt)
	}
	var tombstones []string
	for _, version := range doomedVersions {
		tombstones = append(tombstones, version+":"+deletedAt+":"+hashes[version])
	}
	if got := queryStrings(t, store, "SELECT id || ':' || content_deleted_at || ':' || content_hash FROM memory_record_versions WHERE record_id = 'doomed' AND content IS NULL AND search_text IS NULL ORDER BY id"); !slices.Equal(got, tombstones) {
		t.Fatalf("tombstones = %v", got)
	}
	if got := queryStrings(t, store, "SELECT id FROM memory_sources ORDER BY id"); !slices.Equal(got, []string{"keeper-spec"}) {
		t.Fatalf("sources = %v", got)
	}
	if got := queryStrings(t, store, "SELECT id || ':' || decision || ':' || actor || ':' || reviewer || ':' || reviewed_at || ':' || version_id || ':' || content_hash || ':' || coalesce(reason, '<NULL>') FROM memory_reviews ORDER BY id"); !slices.Equal(got, []string{
		"agent-v2:opinion:agent:reviewer-agent:" + memoryTime + ":doomed-v2:" + hashes["doomed-v2"] + ":<NULL>",
		"approve-v3:approve:human:reviewer-human:" + memoryTime + ":doomed-v3:" + hashes["doomed-v3"] + ":<NULL>",
		"dismiss-v3:dismiss:human:reviewer-human:" + memoryTime + ":doomed-v3:" + hashes["doomed-v3"] + ":<NULL>",
		"keeper-reject:reject:human:reviewer-human:" + memoryTime + ":keeper-v1:sha256:keeper:остава",
		"reject-v1:reject:human:reviewer-human:" + memoryTime + ":doomed-v1:" + hashes["doomed-v1"] + ":<NULL>",
		"system-v2:opinion:system:reviewer-system:" + memoryTime + ":doomed-v2:" + hashes["doomed-v2"] + ":<NULL>",
	}) {
		t.Fatalf("reviews = %v", got)
	}
	// The dismissal row stays with its action; only its reason is cleared.
	if got := queryStrings(t, store, "SELECT id || ':' || suggestion FROM memory_reviews WHERE suggestion IS NOT NULL"); !slices.Equal(got, []string{"dismiss-v3:archive"}) {
		t.Fatalf("suggestions = %v", got)
	}
	if got := queryStrings(t, store, "SELECT from_record_id || '>' || to_record_id || ':' || kind FROM memory_relations"); !slices.Equal(got, []string{"keeper>doomed:contradicts"}) {
		t.Fatalf("relations = %v", got)
	}
	if got := queryStrings(t, store, "SELECT status || ':' || current_version_id || ':' || version || ':' || updated_at || ':' || created_at FROM memory_records WHERE id = 'doomed'"); !slices.Equal(got, []string{"active:doomed-v3:4:" + deletedAt + ":" + memoryTime}) {
		t.Fatalf("record = %v", got)
	}
	receipts := queryStrings(t, store, "SELECT project_id || ':' || subject_kind || ':' || subject_id || ':' || content_hash || ':' || deleted_at || ':' || id FROM context_deletion_receipts ORDER BY subject_id")
	if len(receipts) != 3 {
		t.Fatalf("receipts = %v", receipts)
	}
	for index, version := range doomedVersions {
		want := fmt.Sprintf("project:memory_version:%s:%s:%s:%s", version, hashes[version], deletedAt, result.Receipts[index].ID)
		if receipts[index] != want || !strings.HasPrefix(result.Receipts[index].ID, "deletion-") {
			t.Fatalf("receipt %d = %s, want %s", index, receipts[index], want)
		}
	}

	dumpAfter := dumpMemoryTables(t, store)
	for name, rows := range dumpAfter {
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row), leakMarker) {
				t.Fatalf("deleted text still readable in %s: %s", name, row)
			}
		}
	}
	if strings.Contains(strings.ToLower(strings.Join(slices.Concat(dumpBefore["memory_record_versions"], dumpBefore["memory_sources"], dumpBefore["memory_reviews"]), "\n")), leakMarker) == false {
		t.Fatal("scan did not see the text before deletion")
	}
	if !slices.Equal(dumpBefore["memory_relations"], dumpAfter["memory_relations"]) {
		t.Fatal("relations changed")
	}

	linked, err := store.MemoryReader().ReadLinkedRecords(ctx, "project", []string{"doomed"})
	if err != nil || len(linked) != 1 || !linked[0].ContentDeleted || linked[0].Version.Content != "" || linked[0].ContentBytes != 0 || linked[0].Sources != nil || linked[0].SourceCount != 0 {
		t.Fatalf("linked read after deletion: %+v, %v", linked, err)
	}
	if hits := hitSummary(search(t, store, "project", leakMarker+" паметта", 100)); !slices.Equal(hits, []string{"keeper-v1:1"}) {
		t.Fatalf("search after deletion = %v", hits)
	}
	current := []memory.CurrentRecordState{{
		RecordID: linked[0].Record.ID, Status: linked[0].Record.Status, CurrentVersionID: linked[0].Record.CurrentVersionID,
		RecordVersion: linked[0].Record.Version, ContentDeleted: linked[0].ContentDeleted,
	}}
	recheck := memory.DecideManifestRecheck(memory.ManifestRecheckFacts{Expected: before.Expected, Current: current})
	if recheck.Kind != memory.RecheckRecompose || recheck.Change != memory.ChangeContentDeleted || recheck.RecordID != "doomed" {
		t.Fatalf("recheck = %+v", recheck)
	}
	after, err := composer.ComposeContext(ctx, compose.Request{ProjectID: "project", Snapshot: "snapshot", ExplicitRecordIDs: []string{"doomed"}, Marker: marker})
	if err != nil || after.Kind != compose.BlockedExplicitUnavailable || strings.Contains(after.Part.Text, leakMarker) {
		t.Fatalf("composition after deletion: %s %s %v", after.Kind, after.Reason, err)
	}
}

func TestDeleteRecordAlreadyDeletedWritesNothing(t *testing.T) {
	store := openMemoryStore(t)
	seedDoomed(t, store)
	if got := deleteDoomed(t, store, nil); got.Kind != memory.DeletionDelete {
		t.Fatalf("first deletion = %+v", got)
	}
	before := dumpMemoryTables(t, store)
	for _, expected := range []int64{3, 4} {
		got := deleteDoomed(t, store, func(r *deletion.Request) {
			r.ExpectedVersion, r.Now = expected, deletionNow.Add(time.Hour)
		})
		if got.Kind != memory.DeletionAlreadyDeleted || got.Receipts != nil || got.RecordVersion != 0 {
			t.Fatalf("repeat with expected %d = %+v", expected, got)
		}
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
}

func TestDeleteRecordRefusalsWriteNothing(t *testing.T) {
	store := openMemoryStore(t)
	seedDoomed(t, store)
	mustInsertMemory(t, store.db, newMemoryFixture("foreign", "other"))
	before := dumpMemoryTables(t, store)
	for _, tc := range []struct {
		name   string
		mutate func(*deletion.Request)
		want   memory.RecordDeletionKind
	}{
		{"stale version", func(r *deletion.Request) { r.ExpectedVersion = 2 }, memory.DeletionVersionConflict},
		{"newer version", func(r *deletion.Request) { r.ExpectedVersion = 4 }, memory.DeletionVersionConflict},
		{"foreign project", func(r *deletion.Request) { r.ProjectID = "other" }, memory.DeletionNotFound},
		{"record of another project", func(r *deletion.Request) { r.RecordID, r.ExpectedVersion = "foreign", 1 }, memory.DeletionNotFound},
		{"missing record", func(r *deletion.Request) { r.RecordID = "missing" }, memory.DeletionNotFound},
		{"agent", func(r *deletion.Request) { r.Actor = memory.ActorAgent }, memory.DeletionActorNotAllowed},
		{"system", func(r *deletion.Request) { r.Actor = memory.ActorSystem }, memory.DeletionActorNotAllowed},
		{"unknown actor", func(r *deletion.Request) { r.Actor = "admin" }, memory.DeletionInvalidFacts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deleteDoomed(t, store, tc.mutate); got.Kind != tc.want || got.Receipts != nil {
				t.Fatalf("result = %+v", got)
			}
			assertUnchanged(t, before, dumpMemoryTables(t, store))
		})
	}
}

// Two deleters with the same expected version: the write lock serializes
// them, so one deletes and the other sees the deletion as already done.
func TestConcurrentDeletersDeleteOnce(t *testing.T) {
	store := openMemoryStore(t)
	seedDoomed(t, store)
	service := deletion.Service{Store: store.MemoryDeletion()}
	request := deletion.Request{ProjectID: "project", RecordID: "doomed", ExpectedVersion: 3, Actor: memory.ActorHuman, Now: deletionNow}
	var wait sync.WaitGroup
	results := make([]deletion.Result, 2)
	failures := make([]error, 2)
	for index := range results {
		wait.Go(func() {
			results[index], failures[index] = service.DeleteRecord(context.Background(), request)
		})
	}
	wait.Wait()
	if err := errors.Join(failures...); err != nil {
		t.Fatal(err)
	}
	kinds := []memory.RecordDeletionKind{results[0].Kind, results[1].Kind}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []memory.RecordDeletionKind{memory.DeletionAlreadyDeleted, memory.DeletionDelete}) {
		t.Fatalf("kinds = %v", kinds)
	}
	if got := queryStrings(t, store, "SELECT CAST(count(*) AS TEXT) FROM context_deletion_receipts"); !slices.Equal(got, []string{"3"}) {
		t.Fatalf("receipts = %v", got)
	}
	if got := queryStrings(t, store, "SELECT CAST(version AS TEXT) FROM memory_records WHERE id = 'doomed'"); !slices.Equal(got, []string{"4"}) {
		t.Fatalf("record version = %v", got)
	}
}

// Guarded writes that do not match the facts fail and roll back every write
// of the transaction.
func TestDeleteRecordContentRollsBackOnDivergedFacts(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	seedDoomed(t, store)
	before := dumpMemoryTables(t, store)
	versions := []memory.VersionDeletionState{{VersionID: "doomed-v1", ContentHash: "unused"}}
	for name, d := range map[string]deletion.Deletion{
		"stale record version": {ProjectID: "project", RecordID: "doomed", ExpectedVersion: 2, Versions: versions, DeletedAt: deletionNow},
		"foreign project":      {ProjectID: "other", RecordID: "doomed", ExpectedVersion: 3, Versions: versions, DeletedAt: deletionNow},
		"unknown version":      {ProjectID: "project", RecordID: "doomed", ExpectedVersion: 3, Versions: []memory.VersionDeletionState{{VersionID: "keeper-v1", ContentHash: "sha256:keeper"}}, DeletedAt: deletionNow},
	} {
		err := store.MemoryDeletion().WithinRecordDeletion(ctx, func(tx deletion.Transaction) error {
			return tx.DeleteRecordContent(ctx, d)
		})
		if !errors.Is(err, ErrMemoryDeletionDiverged) {
			t.Fatalf("%s: %v", name, err)
		}
		assertUnchanged(t, before, dumpMemoryTables(t, store))
	}
	if got := deleteDoomed(t, store, nil); got.Kind != memory.DeletionDelete {
		t.Fatalf("deletion after rollbacks = %+v", got)
	}
	err := store.MemoryDeletion().WithinRecordDeletion(ctx, func(tx deletion.Transaction) error {
		return tx.DeleteRecordContent(ctx, deletion.Deletion{ProjectID: "project", RecordID: "doomed", ExpectedVersion: 4, Versions: versions, DeletedAt: deletionNow})
	})
	if !errors.Is(err, ErrMemoryDeletionDiverged) {
		t.Fatalf("deleting a deleted version: %v", err)
	}
}

// A failed receipt insert, after every other write, rolls all of them back;
// the error names no stored value.
func TestDeleteRecordContentRollsBackOnFailedReceipt(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	seedDoomed(t, store)
	before := dumpMemoryTables(t, store)
	_, err := deletion.Service{Store: store.MemoryDeletion(), NewID: func(string) (string, error) { return "same-" + leakMarker, nil }}.
		DeleteRecord(ctx, deletion.Request{ProjectID: "project", RecordID: "doomed", ExpectedVersion: 3, Actor: memory.ActorHuman, Now: deletionNow})
	if err == nil {
		t.Fatal("duplicate receipt id accepted")
	}
	if strings.Contains(strings.ToLower(err.Error()), leakMarker) {
		t.Fatalf("error carries a stored value: %v", err)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
	if got := deleteDoomed(t, store, nil); got.Kind != memory.DeletionDelete {
		t.Fatalf("deletion after rollback = %+v", got)
	}
}

func TestDeletionReadsHonourCancellation(t *testing.T) {
	store := openMemoryStore(t)
	seedDoomed(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(ctx, deletion.Request{ProjectID: "project", RecordID: "doomed", ExpectedVersion: 3, Actor: memory.ActorHuman, Now: deletionNow})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled deletion: %v", err)
	}
	if got := deleteDoomed(t, store, nil); got.Kind != memory.DeletionDelete {
		t.Fatalf("deletion after cancellation = %+v", got)
	}
}

func TestStoredTimeIsFixedWidthUTC(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 11, 10, 0, 0, 5, time.UTC),
		time.Date(2026, 11, 11, 12, 0, 0, 500_000_000, time.FixedZone("EET", 2*60*60)),
	}
	var formatted []string
	for _, value := range times {
		text := formatStoredTime(value)
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil || !parsed.Equal(value) || len(text) != len("2026-11-11T10:00:00.000000000Z") {
			t.Fatalf("%v -> %q, %v", value, text, err)
		}
		formatted = append(formatted, text)
	}
	if !slices.IsSorted(formatted) {
		t.Fatalf("text order differs from time order: %v", formatted)
	}
}

// pausingDeletion runs afterRead between a transaction's fact read and its
// decision.
type pausingDeletion struct {
	deletion.Store
	afterRead func()
}

func (p pausingDeletion) WithinRecordDeletion(ctx context.Context, operation func(deletion.Transaction) error) error {
	return p.Store.WithinRecordDeletion(ctx, func(tx deletion.Transaction) error {
		return operation(pausingTransaction{Transaction: tx, afterRead: p.afterRead})
	})
}

type pausingTransaction struct {
	deletion.Transaction
	afterRead func()
}

func (p pausingTransaction) ReadRecordDeletionFacts(ctx context.Context, recordID string) (deletion.RecordFacts, error) {
	facts, err := p.Transaction.ReadRecordDeletionFacts(ctx, recordID)
	p.afterRead()
	return facts, err
}

// Two stores on one file have separate connections. The first deleter holds
// the write lock from before its read, so the second cannot read the same
// facts until the first commits, and then finds the record deleted.
func TestDeletersOnTwoConnectionsSerializeOnWriteLock(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	seedDoomed(t, first)
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	request := deletion.Request{ProjectID: "project", RecordID: "doomed", ExpectedVersion: 3, Actor: memory.ActorHuman, Now: deletionNow}
	type outcome struct {
		result deletion.Result
		err    error
	}
	secondDone := make(chan outcome, 1)
	paused := pausingDeletion{Store: first.MemoryDeletion(), afterRead: func() {
		go func() {
			result, err := deletion.Service{Store: second.MemoryDeletion()}.DeleteRecord(ctx, request)
			secondDone <- outcome{result, err}
		}()
		select {
		case got := <-secondDone:
			t.Errorf("second deleter finished while the first held the write lock: %+v", got)
			secondDone <- got
		case <-time.After(200 * time.Millisecond):
		}
	}}
	got, err := deletion.Service{Store: paused}.DeleteRecord(ctx, request)
	if err != nil || got.Kind != memory.DeletionDelete {
		t.Fatalf("first deleter: %+v, %v", got, err)
	}
	other := <-secondDone
	if other.err != nil || other.result.Kind != memory.DeletionAlreadyDeleted {
		t.Fatalf("second deleter: %+v, %v", other.result, other.err)
	}
	if got := queryStrings(t, first, "SELECT CAST(count(*) AS TEXT) FROM context_deletion_receipts"); !slices.Equal(got, []string{"3"}) {
		t.Fatalf("receipts = %v", got)
	}
}
