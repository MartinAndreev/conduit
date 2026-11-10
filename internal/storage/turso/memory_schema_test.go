package turso

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/model"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
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
		record: model.MemoryRecords{ID: id, ProjectID: project, Category: "architecture_decision", Status: string(memory.StatusActive), CurrentVersionID: id + "-v1", Version: 1, CreatedAt: memoryTime, UpdatedAt: memoryTime},
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

func TestReviewNamesExactVersionAndHash(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	fixture := newMemoryFixture("reviewed", "project")
	mustInsertMemory(t, store.db, fixture)
	review := func(id, hash string) error {
		query, args := table.MemoryReviews.INSERT(table.MemoryReviews.AllColumns).MODEL(model.MemoryReviews{ID: id, VersionID: fixture.version.ID, ContentHash: hash, Decision: "approve", Reviewer: "martin", ReviewedAt: memoryTime}).Sql()
		_, err := store.db.ExecContext(ctx, query, args...)
		return err
	}
	if err := review("matching", fixture.version.ContentHash); err != nil {
		t.Fatal(err)
	}
	if review("stale", "sha256:other") == nil {
		t.Fatal("review accepted a content hash that is not the version's")
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
	if err := relate("project", "new", "old", "supersedes"); err != nil {
		t.Fatal(err)
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
