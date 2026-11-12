package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/model"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
	jet "github.com/go-jet/jet/v2/sqlite"
)

const memoryTime = "2026-11-10T09:00:00Z"

func openMemoryStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func optional(value string) *string { return &value }

// memoryFixture is a valid human-origin record with its first version.
type memoryFixture struct {
	record  model.MemoryRecords
	version model.MemoryRecordVersions
}

func newMemoryFixture(id, project string) memoryFixture {
	content := "Решение: ПАМЕТТА се пази в Turso"
	return memoryFixture{
		record: model.MemoryRecords{ID: id, ProjectID: project, Category: string(memory.CategoryDecision), Status: string(memory.StatusActive), CurrentVersionID: id + "-v1", Version: 1, CreatedAt: memoryTime, UpdatedAt: memoryTime},
		version: model.MemoryRecordVersions{
			ID: id + "-v1", RecordID: id, ProjectID: project, Content: &content, ContentHash: "sha256:" + id,
			SearchText: optional(normalizeSearchText(content)), SearchNormalizationVersion: searchNormalizationVersion,
			Author: "martin", Trust: string(memory.TrustHumanApproved), RedactionPolicyVersion: "1",
			Visibility: string(memory.VisibilityProject), Origin: string(memory.OriginHuman), CreatedAt: memoryTime,
		},
	}
}

func insertRecord(ctx context.Context, db database, record model.MemoryRecords) error {
	query, args := table.MemoryRecords.INSERT(table.MemoryRecords.AllColumns).MODEL(record).Sql()
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func insertVersion(ctx context.Context, db database, version model.MemoryRecordVersions) error {
	query, args := table.MemoryRecordVersions.INSERT(table.MemoryRecordVersions.AllColumns).MODEL(version).Sql()
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func insertSource(ctx context.Context, db database, source model.MemorySources) error {
	query, args := table.MemorySources.INSERT(table.MemorySources.AllColumns).MODEL(source).Sql()
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

// inTransaction commits operation's writes on a pinned connection; deferred
// constraints are checked at COMMIT. Turso keeps the transaction open when
// COMMIT fails and database/sql's Tx does not roll it back, which would leave
// the pooled connection inside a transaction, so failure rolls back here.
func inTransaction(ctx context.Context, db *sql.DB, operation func(database) error) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return err
	}
	if err = operation(conn); err == nil {
		_, err = conn.ExecContext(ctx, "COMMIT")
	}
	if err != nil {
		if _, rollbackErr := conn.ExecContext(context.Background(), "ROLLBACK"); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
	}
	return err
}

func insertMemory(ctx context.Context, db *sql.DB, fixture memoryFixture) error {
	return inTransaction(ctx, db, func(tx database) error {
		if err := insertRecord(ctx, tx, fixture.record); err != nil {
			return err
		}
		return insertVersion(ctx, tx, fixture.version)
	})
}

func mustInsertMemory(t *testing.T, db *sql.DB, fixture memoryFixture) {
	t.Helper()
	if err := insertMemory(context.Background(), db, fixture); err != nil {
		t.Fatal(err)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	store := openMemoryStore(t)
	var enabled int
	if err := store.db.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign_keys = %d, %v", enabled, err)
	}
}

// The CHECK lists are literal DDL; these tests keep them equal to the domain
// owner's enumerations.
func TestMemoryEnumerationsMatchDomain(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	for _, status := range memory.RecordStatuses() {
		fixture := newMemoryFixture("status-"+string(status), "project")
		fixture.record.Status = string(status)
		if err := insertMemory(ctx, store.db, fixture); err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
	}
	unknownStatus := newMemoryFixture("status-unknown", "project")
	unknownStatus.record.Status = "deleted"
	if insertMemory(ctx, store.db, unknownStatus) == nil {
		t.Fatal("unknown status accepted")
	}
	for _, trust := range memory.Trusts() {
		fixture := newMemoryFixture("trust-"+string(trust), "project")
		fixture.version.Trust = string(trust)
		if err := insertMemory(ctx, store.db, fixture); err != nil {
			t.Fatalf("trust %s: %v", trust, err)
		}
	}
	for _, category := range memory.Categories() {
		fixture := newMemoryFixture("category-"+string(category), "project")
		fixture.record.Category = string(category)
		if err := insertMemory(ctx, store.db, fixture); err != nil {
			t.Fatalf("category %s: %v", category, err)
		}
	}
	unknownCategory := newMemoryFixture("category-unknown", "project")
	unknownCategory.record.Category = "architecture_decision"
	if insertMemory(ctx, store.db, unknownCategory) == nil {
		t.Fatal("unknown category accepted")
	}
	unknownTrust := newMemoryFixture("trust-unknown", "project")
	unknownTrust.version.Trust = "backend_score"
	if insertMemory(ctx, store.db, unknownTrust) == nil {
		t.Fatal("unknown trust accepted")
	}
	base := newMemoryFixture("sources", "project")
	mustInsertMemory(t, store.db, base)
	for index, kind := range []memory.SourceKind{memory.SourceSpec, memory.SourceFile, memory.SourceEvent, memory.SourceAssignment} {
		source := model.MemorySources{ID: "source-" + string(kind), VersionID: base.version.ID, Kind: string(kind), Ref: optional("ref")}
		if kind == memory.SourceFile {
			source.Ref, source.Path, source.BlobHash = nil, optional("internal/memory/types.go"), optional("blob")
		}
		if err := insertSource(ctx, store.db, source); err != nil {
			t.Fatalf("source %d %s: %v", index, kind, err)
		}
	}
	if insertSource(ctx, store.db, model.MemorySources{ID: "source-unknown", VersionID: base.version.ID, Kind: "url", Ref: optional("ref")}) == nil {
		t.Fatal("unknown source kind accepted")
	}
}

func TestRecordAndCurrentVersionIntegrity(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	mustInsertMemory(t, store.db, newMemoryFixture("record", "project"))

	withoutVersion := newMemoryFixture("no-version", "project")
	if err := inTransaction(ctx, store.db, func(tx database) error { return insertRecord(ctx, tx, withoutVersion.record) }); err == nil {
		t.Fatal("record committed without its current version")
	}
	foreignProject := newMemoryFixture("foreign-version", "project")
	foreignProject.version.ProjectID = "other-project"
	if insertMemory(ctx, store.db, foreignProject) == nil {
		t.Fatal("version accepted under a different project than its record")
	}
	borrowed := newMemoryFixture("borrowed", "project")
	borrowed.record.CurrentVersionID = "record-v1"
	borrowed.version.ID = "borrowed-v1"
	if insertMemory(ctx, store.db, borrowed) == nil {
		t.Fatal("record accepted another record's version as current")
	}
	zeroVersion := newMemoryFixture("zero", "project")
	zeroVersion.record.Version = 0
	if insertMemory(ctx, store.db, zeroVersion) == nil {
		t.Fatal("optimistic version 0 accepted")
	}
	second := newMemoryFixture("record", "project").version
	second.ID, second.ContentHash = "record-v2", "sha256:record-v2"
	if err := insertVersion(ctx, store.db, second); err != nil {
		t.Fatalf("second version: %v", err)
	}
	result, err := store.db.ExecContext(ctx, "UPDATE memory_records SET current_version_id = ?, version = version + 1 WHERE id = ? AND version = ?", "record-v2", "record", 1)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		t.Fatalf("optimistic update changed %d rows", changed)
	}
}

func TestVersionScopeAndContentConstraints(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	attempt := newMemoryFixture("attempt", "project")
	attempt.version.Origin, attempt.version.Visibility = string(memory.OriginAttempt), string(memory.VisibilityAttempt)
	attempt.version.RunID, attempt.version.AttemptID, attempt.version.BaseCommit = optional("run"), optional("attempt"), optional("abc123")
	mustInsertMemory(t, store.db, attempt)
	accepted := newMemoryFixture("accepted", "project")
	accepted.version.Origin = string(memory.OriginAttempt)
	accepted.version.RunID, accepted.version.AttemptID, accepted.version.BaseCommit = optional("run"), optional("attempt"), optional("abc123")
	accepted.version.AcceptedResultRef = optional("result")
	mustInsertMemory(t, store.db, accepted)

	for name, mutate := range map[string]func(*model.MemoryRecordVersions){
		"human with run":     func(v *model.MemoryRecordVersions) { v.RunID = optional("run") },
		"human with attempt": func(v *model.MemoryRecordVersions) { v.AttemptID = optional("attempt") },
		"attempt without base": func(v *model.MemoryRecordVersions) {
			v.Origin, v.RunID, v.AttemptID = "attempt", optional("run"), optional("attempt")
		},
		"attempt without attempt": func(v *model.MemoryRecordVersions) {
			v.Origin, v.RunID, v.BaseCommit = "attempt", optional("run"), optional("abc")
		},
		"empty run instead of null": func(v *model.MemoryRecordVersions) {
			v.Origin, v.RunID, v.AttemptID, v.BaseCommit = "attempt", optional(""), optional("a"), optional("b")
		},
		"accepted attempt without result": func(v *model.MemoryRecordVersions) {
			v.Origin, v.RunID, v.AttemptID, v.BaseCommit = "attempt", optional("run"), optional("attempt"), optional("abc")
		},
		"unknown visibility":      func(v *model.MemoryRecordVersions) { v.Visibility = "global" },
		"unknown origin":          func(v *model.MemoryRecordVersions) { v.Origin = "agent" },
		"content without search":  func(v *model.MemoryRecordVersions) { v.SearchText = nil },
		"deleted with content":    func(v *model.MemoryRecordVersions) { v.ContentDeletedAt = optional(memoryTime) },
		"normalization version 0": func(v *model.MemoryRecordVersions) { v.SearchNormalizationVersion = 0 },
		"empty content hash":      func(v *model.MemoryRecordVersions) { v.ContentHash = "" },
		"empty redaction version": func(v *model.MemoryRecordVersions) { v.RedactionPolicyVersion = "" },
	} {
		fixture := newMemoryFixture("invalid", "project")
		mutate(&fixture.version)
		if insertMemory(ctx, store.db, fixture) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	tombstoned := newMemoryFixture("tombstoned", "project")
	mustInsertMemory(t, store.db, tombstoned)
	if _, err := store.db.ExecContext(ctx, "UPDATE memory_record_versions SET content = NULL, search_text = NULL, content_deleted_at = ? WHERE id = ?", memoryTime, tombstoned.version.ID); err != nil {
		t.Fatalf("content tombstone: %v", err)
	}
}

func TestFileSourcePathIsUniquePerVersion(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	first, second := newMemoryFixture("first", "project"), newMemoryFixture("second", "project")
	mustInsertMemory(t, store.db, first)
	mustInsertMemory(t, store.db, second)
	file := func(id, version string) model.MemorySources {
		return model.MemorySources{ID: id, VersionID: version, Kind: string(memory.SourceFile), Path: optional("internal/memory/types.go"), BlobHash: optional("blob-" + id)}
	}
	if err := insertSource(ctx, store.db, file("a", first.version.ID)); err != nil {
		t.Fatal(err)
	}
	if insertSource(ctx, store.db, file("b", first.version.ID)) == nil {
		t.Fatal("duplicate path accepted within one version")
	}
	if err := insertSource(ctx, store.db, file("c", second.version.ID)); err != nil {
		t.Fatalf("same path in another version: %v", err)
	}
	for _, id := range []string{"spec-1", "spec-2"} {
		if err := insertSource(ctx, store.db, model.MemorySources{ID: id, VersionID: first.version.ID, Kind: string(memory.SourceSpec), Ref: optional(id)}); err != nil {
			t.Fatalf("non-file source %s: %v", id, err)
		}
	}
	for name, source := range map[string]model.MemorySources{
		"file without blob hash": {ID: "x1", VersionID: first.version.ID, Kind: "file", Path: optional("other.go")},
		"file without path":      {ID: "x2", VersionID: first.version.ID, Kind: "file", BlobHash: optional("blob")},
		"spec with path":         {ID: "x3", VersionID: first.version.ID, Kind: "spec", Ref: optional("spec"), Path: optional("spec.md")},
		"event without ref":      {ID: "x4", VersionID: first.version.ID, Kind: "event"},
		"unknown version":        {ID: "x5", VersionID: "missing", Kind: "spec", Ref: optional("spec")},
	} {
		if insertSource(ctx, store.db, source) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func insertReview(ctx context.Context, db database, review model.MemoryReviews) error {
	query, args := table.MemoryReviews.INSERT(table.MemoryReviews.AllColumns).MODEL(review).Sql()
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func newReview(id string, version model.MemoryRecordVersions, actor memory.Actor, decision memory.ReviewDecision) model.MemoryReviews {
	return model.MemoryReviews{ID: id, VersionID: version.ID, ContentHash: version.ContentHash, Decision: string(decision), Actor: string(actor), Reviewer: "reviewer-" + string(actor), ReviewedAt: memoryTime}
}

func TestReviewNamesExactVersionAndHash(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	fixture := newMemoryFixture("reviewed", "project")
	mustInsertMemory(t, store.db, fixture)
	if err := insertReview(ctx, store.db, newReview("matching", fixture.version, memory.ActorHuman, memory.ReviewApprove)); err != nil {
		t.Fatal(err)
	}
	stale := newReview("stale", fixture.version, memory.ActorHuman, memory.ReviewApprove)
	stale.ContentHash = "sha256:other"
	if insertReview(ctx, store.db, stale) == nil {
		t.Fatal("review accepted a content hash that is not the version's")
	}
}

// The review CHECK lists are literal DDL; this keeps them equal to the domain
// owner's actors and decisions, and only a human may approve or reject.
func TestReviewActorsAndDecisionsMatchDomain(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	fixture := newMemoryFixture("reviewed", "project")
	mustInsertMemory(t, store.db, fixture)
	for _, actor := range memory.Actors() {
		for _, decision := range memory.ReviewDecisions() {
			err := insertReview(ctx, store.db, newReview(string(actor)+"-"+string(decision), fixture.version, actor, decision))
			allowed := actor == memory.ActorHuman || decision == memory.ReviewOpinion
			if allowed != (err == nil) {
				t.Fatalf("%s %s: allowed=%v, err=%v", actor, decision, allowed, err)
			}
		}
	}
	for name, review := range map[string]model.MemoryReviews{
		"unknown actor":    newReview("x1", fixture.version, "reviewer_bot", memory.ReviewOpinion),
		"empty actor":      newReview("x2", fixture.version, "", memory.ReviewOpinion),
		"unknown decision": newReview("x3", fixture.version, memory.ActorHuman, "approved"),
	} {
		if insertReview(ctx, store.db, review) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestRelationsStayWithinOneProject(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	for _, fixture := range []memoryFixture{newMemoryFixture("old", "project"), newMemoryFixture("new", "project"), newMemoryFixture("foreign", "other")} {
		mustInsertMemory(t, store.db, fixture)
	}
	relate := func(project, from, to, kind string) error {
		_, err := store.db.ExecContext(ctx, "INSERT INTO memory_relations (project_id, from_record_id, to_record_id, kind, created_at) VALUES (?, ?, ?, ?, ?)", project, from, to, kind, memoryTime)
		return err
	}
	// The kind CHECK mirrors memory.RelationKinds.
	for _, kind := range memory.RelationKinds() {
		if err := relate("project", "new", "old", string(kind)); err != nil {
			t.Fatalf("relation %s: %v", kind, err)
		}
	}
	for name, attempt := range map[string][4]string{
		"cross project":   {"project", "new", "foreign", "contradicts"},
		"foreign project": {"other", "new", "old", "contradicts"},
		"self":            {"project", "old", "old", "contradicts"},
		"unknown kind":    {"project", "old", "new", "duplicates"},
	} {
		if relate(attempt[0], attempt[1], attempt[2], attempt[3]) == nil {
			t.Fatalf("%s relation accepted", name)
		}
	}
}

func TestContextManifestConstraints(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	manifest := func(id, intent, status string) error {
		_, err := store.db.ExecContext(ctx, `INSERT INTO context_manifests (id, project_id, run_id, attempt_id, assignment_id, revision, intent_id, payload_hash, redaction_policy_version, search_status, created_at)
			VALUES (?, 'project', 'run', 'attempt', 'assignment', 3, ?, 'sha256:payload', '1', ?, ?)`, id, intent, status, memoryTime)
		return err
	}
	if err := manifest("manifest", "intent", "ok"); err != nil {
		t.Fatal(err)
	}
	if manifest("duplicate", "intent", "ok") == nil {
		t.Fatal("second manifest accepted for the same dispatch request")
	}
	if manifest("bad-status", "other-intent", "unknown") == nil {
		t.Fatal("unknown search status accepted")
	}
	item := func(ordinal int, mode string, trust any) error {
		_, err := store.db.ExecContext(ctx, "INSERT INTO context_manifest_items (manifest_id, ordinal, source_identity, source_version, mode, reason, trust) VALUES ('manifest', ?, 'record', 'record-v1', ?, 'eligible', ?)", ordinal, mode, trust)
		return err
	}
	if err := item(0, "included_text", string(memory.TrustHumanApproved)); err != nil {
		t.Fatal(err)
	}
	if err := item(1, "referenced", nil); err != nil {
		t.Fatalf("item without trust: %v", err)
	}
	if item(2, "summarized", nil) == nil || item(3, "referenced", "score") == nil {
		t.Fatal("unknown item mode or trust accepted")
	}
	if _, err := store.db.ExecContext(ctx, "INSERT INTO context_manifest_payloads (manifest_id, content, status) VALUES ('manifest', 'text', 'present')"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE context_manifest_payloads SET status = 'removed_after_delivery' WHERE manifest_id = 'manifest'"); err == nil {
		t.Fatal("removed payload kept its text")
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE context_manifest_payloads SET status = 'removed_after_delivery', content = NULL, removed_at = ? WHERE manifest_id = 'manifest'", memoryTime); err != nil {
		t.Fatalf("payload removal: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "INSERT INTO context_manifest_payloads (manifest_id, content, status) VALUES ('missing', 'text', 'present')"); err == nil {
		t.Fatal("payload accepted without a manifest")
	}
}

// Turso folds only ASCII in lower() and LIKE, so search compares the stored
// Go-normalized text with a query normalized the same way.
func TestSearchTextMatchesUnicodeCaseInsensitively(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	mustInsertMemory(t, store.db, newMemoryFixture("here", "project"))
	mustInsertMemory(t, store.db, newMemoryFixture("elsewhere", "other"))
	search := func(column jet.ColumnString, term string) []string {
		statement := jet.SELECT(table.MemoryRecordVersions.ID).FROM(table.MemoryRecordVersions).WHERE(
			table.MemoryRecordVersions.ProjectID.EQ(jet.String("project")).
				AND(table.MemoryRecordVersions.SearchNormalizationVersion.EQ(jet.Int(searchNormalizationVersion))).
				AND(column.LIKE(jet.String("%" + term + "%"))),
		)
		query, args := statement.Sql()
		rows, err := store.db.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		return ids
	}
	if got := search(table.MemoryRecordVersions.SearchText, normalizeSearchText("Паметта")); !slices.Equal(got, []string{"here-v1"}) {
		t.Fatalf("normalized search = %v", got)
	}
	if got := search(table.MemoryRecordVersions.Content, "паметта"); len(got) != 0 {
		t.Fatalf("raw content LIKE folded non-ASCII case: %v", got)
	}
}

// Evidence for repository work: when a deferred constraint fails at COMMIT,
// Turso keeps the transaction open and database/sql's Tx does not roll it
// back, so the store's single pooled connection stays inside it.
func TestFailedDeferredCommitThroughSQLTxLeavesTransactionOpen(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertRecord(ctx, tx, newMemoryFixture("orphan", "project").record); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("record committed without its current version")
	}
	if _, err := store.db.ExecContext(ctx, "BEGIN"); err == nil {
		t.Fatal("connection was not left inside the failed transaction")
	}
	if _, err := store.db.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	mustInsertMemory(t, store.db, newMemoryFixture("after-rollback", "project"))
}

// fill is a value of exactly size UTF-8 bytes starting with prefix, mostly
// two-byte Cyrillic, so a byte bound is not mistaken for a character bound.
func fill(prefix string, size int) string {
	rest := size - len(prefix)
	return prefix + strings.Repeat("я", rest/2) + strings.Repeat("a", rest%2)
}

// Every free-text memory column accepts a value at its byte bound and refuses
// one byte more.
func TestMemoryColumnsHaveByteBounds(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	type bound struct {
		name string
		max  int
		set  func(*memoryFixture, string)
	}
	attempt := func(f *memoryFixture) {
		f.version.Origin, f.version.Visibility = string(memory.OriginAttempt), string(memory.VisibilityAttempt)
		f.version.RunID, f.version.AttemptID, f.version.BaseCommit = optional("run"), optional("attempt"), optional("abc")
	}
	records := []bound{
		{"record id", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { f.record.ID, f.version.RecordID = v, v }},
		{"project id", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { f.record.ProjectID, f.version.ProjectID = v, v }},
		{"record created_at", schema.MaxTimestampBytes, func(f *memoryFixture, v string) { f.record.CreatedAt = v }},
		{"record updated_at", schema.MaxTimestampBytes, func(f *memoryFixture, v string) { f.record.UpdatedAt = v }},
		{"version id", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { f.version.ID, f.record.CurrentVersionID = v, v }},
		{"content hash", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { f.version.ContentHash = v }},
		{"content deleted at", schema.MaxTimestampBytes, func(f *memoryFixture, v string) {
			f.version.Content, f.version.SearchText, f.version.ContentDeletedAt = nil, nil, &v
		}},
		{"author", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { f.version.Author = v }},
		{"redaction policy version", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { f.version.RedactionPolicyVersion = v }},
		{"run id", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { attempt(f); f.version.RunID = &v }},
		{"attempt id", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { attempt(f); f.version.AttemptID = &v }},
		{"base commit", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { attempt(f); f.version.BaseCommit = &v }},
		{"accepted result ref", schema.MaxIdentityBytes, func(f *memoryFixture, v string) { attempt(f); f.version.AcceptedResultRef = &v }},
		{"version created_at", schema.MaxTimestampBytes, func(f *memoryFixture, v string) { f.version.CreatedAt = v }},
	}
	for index, b := range records {
		for _, size := range []int{b.max, b.max + 1} {
			fixture := newMemoryFixture(fmt.Sprintf("bound-%02d-%d", index, size), "project")
			b.set(&fixture, fill(fmt.Sprintf("b%02d", index), size))
			if err := insertMemory(ctx, store.db, fixture); (err == nil) != (size == b.max) {
				t.Fatalf("%s with %d bytes: %v", b.name, size, err)
			}
		}
	}
	base := newMemoryFixture("bound-base", "project")
	mustInsertMemory(t, store.db, base)
	mustInsertMemory(t, store.db, newMemoryFixture("bound-other", "project"))
	others := []struct {
		name   string
		max    int
		insert func(id, v string) error
	}{
		{"source id", schema.MaxIdentityBytes, func(_, v string) error {
			return insertSource(ctx, store.db, model.MemorySources{ID: v, VersionID: base.version.ID, Kind: "spec", Ref: optional("spec")})
		}},
		{"source ref", schema.MaxTextFieldBytes, func(id, v string) error {
			return insertSource(ctx, store.db, model.MemorySources{ID: id, VersionID: base.version.ID, Kind: "spec", Ref: &v})
		}},
		{"source path", schema.MaxTextFieldBytes, func(id, v string) error {
			return insertSource(ctx, store.db, model.MemorySources{ID: id, VersionID: base.version.ID, Kind: "file", Path: &v, BlobHash: optional("blob")})
		}},
		{"source blob hash", schema.MaxIdentityBytes, func(id, v string) error {
			return insertSource(ctx, store.db, model.MemorySources{ID: id, VersionID: base.version.ID, Kind: "file", Path: optional(id), BlobHash: &v})
		}},
		{"review id", schema.MaxIdentityBytes, func(_, v string) error {
			return insertReview(ctx, store.db, newReview(v, base.version, memory.ActorHuman, memory.ReviewApprove))
		}},
		{"reviewer", schema.MaxIdentityBytes, func(id, v string) error {
			review := newReview(id, base.version, memory.ActorHuman, memory.ReviewApprove)
			review.Reviewer = v
			return insertReview(ctx, store.db, review)
		}},
		{"review reason", schema.MaxTextFieldBytes, func(id, v string) error {
			review := newReview(id, base.version, memory.ActorHuman, memory.ReviewReject)
			review.Reason = &v
			return insertReview(ctx, store.db, review)
		}},
		{"reviewed at", schema.MaxTimestampBytes, func(id, v string) error {
			review := newReview(id, base.version, memory.ActorAgent, memory.ReviewOpinion)
			review.ReviewedAt = v
			return insertReview(ctx, store.db, review)
		}},
		{"relation created_at", schema.MaxTimestampBytes, func(id, v string) error {
			kind := map[bool]string{true: "supersedes", false: "contradicts"}[strings.HasSuffix(id, "a")]
			_, err := store.db.ExecContext(ctx, "INSERT INTO memory_relations (project_id, from_record_id, to_record_id, kind, created_at) VALUES ('project', 'bound-base', 'bound-other', ?, ?)", kind, v)
			return err
		}},
	}
	for index, b := range others {
		for attempt, size := range []int{b.max, b.max + 1} {
			id := fmt.Sprintf("other-%02d-%c", index, 'a'+attempt)
			if err := b.insert(id, fill(fmt.Sprintf("o%02d%c", index, 'a'+attempt), size)); (err == nil) != (size == b.max) {
				t.Fatalf("%s with %d bytes: %v", b.name, size, err)
			}
		}
	}
}

// Content and search text each accept exactly MaxStoredContentBytes and refuse
// one byte more, independently of each other.
func TestStoredContentAndSearchTextAreBounded(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	if schema.MaxStoredContentBytes != memory.MaxStoredContentBytes {
		t.Fatalf("stored content cap = %d", schema.MaxStoredContentBytes)
	}
	atCap := fill("c", schema.MaxStoredContentBytes)
	overCap := fill("c", schema.MaxStoredContentBytes+1)
	for _, tc := range []struct {
		name            string
		content, search string
		accepted        bool
	}{
		{"both at the cap", atCap, atCap, true},
		{"content one byte over", overCap, "c", false},
		{"search text one byte over", atCap, overCap, false},
		{"small content, oversized search text", "кратко", overCap, false},
	} {
		fixture := newMemoryFixture("stored-"+strings.ReplaceAll(tc.name, " ", "-"), "project")
		fixture.version.Content, fixture.version.SearchText = &tc.content, &tc.search
		if err := insertMemory(ctx, store.db, fixture); (err == nil) != tc.accepted {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

// A migrated v1 database takes the bounded memory columns and reopens at the
// current schema.
func TestMigratedDatabaseReopensWithBoundedMemoryColumns(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	if err := createV1Database(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newMemoryFixture("migrated", "project")
	content := fill("m", schema.MaxStoredContentBytes)
	fixture.version.Content, fixture.version.SearchText = &content, &content
	mustInsertMemory(t, store.db, fixture)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer store.Close()
	assertCurrentSchema(t, store.db)
	got, err := store.MemoryReader().ReadLinkedRecords(ctx, "project", []string{"migrated"})
	if err != nil || len(got) != 1 || got[0].ContentBytes != schema.MaxStoredContentBytes || got[0].Version.Content != "" {
		t.Fatalf("migrated record: %d candidates, %v", len(got), err)
	}
}

// contextRows are valid rows of every context_* table, as column -> value.
func contextRows() map[string]map[string]any {
	return map[string]map[string]any{
		"context_manifests": {
			"id": "manifest", "project_id": "project", "run_id": "run", "attempt_id": "attempt", "assignment_id": "assignment",
			"revision": 1, "intent_id": "intent", "payload_hash": "sha256:payload", "redaction_policy_version": "1",
			"search_status": "ok", "created_at": memoryTime,
		},
		"context_manifest_items": {
			"manifest_id": "manifest", "ordinal": 0, "source_identity": "record", "source_version": "record-v1",
			"mode": "included_text", "reason": "eligible", "trust": string(memory.TrustHumanApproved),
		},
		"context_manifest_payloads": {
			"manifest_id": "manifest", "content": nil, "status": string(memory.PayloadRemovedAfterDelivery), "removed_at": memoryTime,
		},
		"context_deletion_receipts": {
			"id": "receipt", "project_id": "project", "subject_kind": string(memory.DeletionSubjectMemoryVersion),
			"subject_id": "record-v1", "content_hash": "sha256:record", "deleted_at": memoryTime,
		},
	}
}

// insertContextRow inserts row into table inside a transaction that is always
// rolled back, after inserting the manifest the other tables reference.
func insertContextRow(ctx context.Context, store *Store, name string, row map[string]any) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	insert := func(name string, row map[string]any) error {
		columns := make([]string, 0, len(row))
		for column := range row {
			columns = append(columns, column)
		}
		slices.Sort(columns)
		values := make([]any, len(columns))
		for index, column := range columns {
			values[index] = row[column]
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
		_, err := tx.ExecContext(ctx, "INSERT INTO "+name+" ("+strings.Join(columns, ", ")+") VALUES ("+placeholders+")", values...)
		return err
	}
	if name != "context_manifests" && name != "context_deletion_receipts" {
		if err := insert("context_manifests", contextRows()["context_manifests"]); err != nil {
			return fmt.Errorf("manifest: %w", err)
		}
	}
	return insert(name, row)
}

// Every free-text context_* column accepts a value at its byte bound and
// refuses one byte more. The payload's content has no bound yet: composition
// has no total limit, and task 4.3 sets it with the payload write.
func TestContextColumnsHaveByteBounds(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	for name, row := range contextRows() {
		if err := insertContextRow(ctx, store, name, row); err != nil {
			t.Fatalf("valid %s row: %v", name, err)
		}
	}
	bounds := map[string]map[string]int{
		"context_manifests": {
			"id": schema.MaxIdentityBytes, "project_id": schema.MaxIdentityBytes, "run_id": schema.MaxIdentityBytes,
			"attempt_id": schema.MaxIdentityBytes, "assignment_id": schema.MaxIdentityBytes, "intent_id": schema.MaxIdentityBytes,
			"payload_hash": schema.MaxIdentityBytes, "redaction_policy_version": schema.MaxIdentityBytes, "created_at": schema.MaxTimestampBytes,
		},
		"context_manifest_items": {
			"source_identity": schema.MaxIdentityBytes, "source_version": schema.MaxIdentityBytes, "reason": schema.MaxTextFieldBytes,
		},
		"context_manifest_payloads": {"removed_at": schema.MaxTimestampBytes},
		"context_deletion_receipts": {
			"id": schema.MaxIdentityBytes, "project_id": schema.MaxIdentityBytes, "subject_id": schema.MaxIdentityBytes,
			"content_hash": schema.MaxIdentityBytes, "deleted_at": schema.MaxTimestampBytes,
		},
	}
	for name, columns := range bounds {
		for column, max := range columns {
			for _, size := range []int{max, max + 1} {
				row := contextRows()[name]
				row[column] = fill("x", size)
				if err := insertContextRow(ctx, store, name, row); (err == nil) != (size == max) {
					t.Fatalf("%s.%s with %d bytes: %v", name, column, size, err)
				}
			}
			row := contextRows()[name]
			row[column] = ""
			if insertContextRow(ctx, store, name, row) == nil {
				t.Fatalf("%s.%s accepted an empty value", name, column)
			}
		}
	}
	payload := contextRows()["context_manifest_payloads"]
	payload["status"], payload["content"], payload["removed_at"] = string(memory.PayloadPresent), fill("p", schema.MaxStoredContentBytes+1), nil
	if err := insertContextRow(ctx, store, "context_manifest_payloads", payload); err != nil {
		t.Fatalf("payload content above the stored content cap: %v", err)
	}
}

// The receipt subject kinds and payload statuses are literal DDL; this keeps
// them equal to the domain owner's lists.
func TestContextEnumerationsMatchDomain(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	for _, kind := range memory.DeletionSubjectKinds() {
		row := contextRows()["context_deletion_receipts"]
		row["subject_kind"] = string(kind)
		if err := insertContextRow(ctx, store, "context_deletion_receipts", row); err != nil {
			t.Fatalf("subject kind %s: %v", kind, err)
		}
	}
	for _, kind := range []string{"memory_record", "", "MEMORY_VERSION"} {
		row := contextRows()["context_deletion_receipts"]
		row["subject_kind"] = kind
		if insertContextRow(ctx, store, "context_deletion_receipts", row) == nil {
			t.Fatalf("subject kind %q accepted", kind)
		}
	}
	for _, status := range memory.PayloadStatuses() {
		row := contextRows()["context_manifest_payloads"]
		row["status"] = string(status)
		if status == memory.PayloadPresent {
			row["content"], row["removed_at"] = "text", nil
		}
		if err := insertContextRow(ctx, store, "context_manifest_payloads", row); err != nil {
			t.Fatalf("payload status %s: %v", status, err)
		}
	}
	row := contextRows()["context_manifest_payloads"]
	row["status"] = "removed"
	if insertContextRow(ctx, store, "context_manifest_payloads", row) == nil {
		t.Fatal("unknown payload status accepted")
	}
}
