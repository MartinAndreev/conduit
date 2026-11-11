package turso

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/childprocess"
)

func doomedRequest() deletion.Request {
	return deletion.Request{ProjectID: "project", RecordID: "doomed", ExpectedVersion: 3, Actor: memory.ActorHuman, Now: deletionNow}
}

// openFaultyDoomed seeds the doomed record in a fresh file and reopens it
// through a fault connector.
func openFaultyDoomed(t *testing.T) (*Store, *faultConnector) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	seeded, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	seedDoomed(t, seeded)
	if err := seeded.Close(); err != nil {
		t.Fatal(err)
	}
	db, connector := openFaulty(t, path, 5000)
	return &Store{db: db}, connector
}

// assertPoolOutsideTransaction proves the pooled connection is not left
// inside a transaction: BEGIN would fail on one that is.
func assertPoolOutsideTransaction(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, "BEGIN"); err != nil {
		t.Fatalf("pooled connection is inside a transaction: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}

// assertDeletedOnce checks the end state after any number of attempts: every
// version deleted once, one receipt per version, one version bump.
func assertDeletedOnce(t *testing.T, store *Store, versions int) {
	t.Helper()
	count := strconv.Itoa(versions)
	if got := queryStrings(t, store, "SELECT CAST(count(*) AS TEXT) FROM context_deletion_receipts WHERE subject_kind = 'memory_version'"); !slices.Equal(got, []string{count}) {
		t.Fatalf("receipts = %v, want %s", got, count)
	}
	if got := queryStrings(t, store, "SELECT CAST(count(DISTINCT subject_id) AS TEXT) FROM context_deletion_receipts"); !slices.Equal(got, []string{count}) {
		t.Fatalf("distinct receipt subjects = %v", got)
	}
	if got := queryStrings(t, store, "SELECT CAST(count(*) AS TEXT) FROM memory_record_versions WHERE record_id = 'doomed' AND content_deleted_at IS NOT NULL AND content IS NULL"); !slices.Equal(got, []string{count}) {
		t.Fatalf("tombstoned versions = %v", got)
	}
	if got := queryStrings(t, store, "SELECT CAST(version AS TEXT) FROM memory_records WHERE id = 'doomed'"); !slices.Equal(got, []string{"4"}) {
		t.Fatalf("record version = %v", got)
	}
}

// retryDeletion repeats the request without faults; it must end delete or
// already_deleted.
func retryDeletion(t *testing.T, store *Store) memory.RecordDeletionKind {
	t.Helper()
	got, err := deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(context.Background(), doomedRequest())
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got.Kind != memory.DeletionDelete && got.Kind != memory.DeletionAlreadyDeleted {
		t.Fatalf("retry = %+v", got)
	}
	return got.Kind
}

// A failed COMMIT is rolled back on the same connection: no result, nothing
// written, the pool is usable, and the retry deletes.
func TestDeletionCommitFailureRollsBack(t *testing.T) {
	store, connector := openFaultyDoomed(t)
	before := dumpMemoryTables(t, store)
	commitFailure := errors.New("injected commit failure")
	connector.inject("COMMIT", commitFailure, 1)
	got, err := deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(context.Background(), doomedRequest())
	if !errors.Is(err, commitFailure) || got.Kind != "" || got.Receipts != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if connector.count("ROLLBACK") != 1 || connector.closes.Load() != 0 {
		t.Fatalf("rollbacks %d, closed connections %d", connector.count("ROLLBACK"), connector.closes.Load())
	}
	assertPoolOutsideTransaction(t, store)
	assertUnchanged(t, before, dumpMemoryTables(t, store))
	if kind := retryDeletion(t, store); kind != memory.DeletionDelete {
		t.Fatalf("retry = %s", kind)
	}
	assertDeletedOnce(t, store, len(doomedVersions))
}

// When COMMIT and ROLLBACK both fail, the transaction state is unknown, so
// the driver connection is discarded rather than pooled; closing it ends the
// engine transaction and nothing is written.
func TestDeletionCommitAndRollbackFailureDiscardsConnection(t *testing.T) {
	store, connector := openFaultyDoomed(t)
	before := dumpMemoryTables(t, store)
	commitFailure, rollbackFailure := errors.New("injected commit failure"), errors.New("injected rollback failure")
	connector.inject("COMMIT", commitFailure, 1)
	connector.inject("ROLLBACK", rollbackFailure, 1)
	got, err := deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(context.Background(), doomedRequest())
	if !errors.Is(err, commitFailure) || !errors.Is(err, rollbackFailure) || got.Kind != "" || got.Receipts != nil {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if connector.closes.Load() != 1 {
		t.Fatalf("closed connections = %d", connector.closes.Load())
	}
	assertPoolOutsideTransaction(t, store)
	if connector.connects.Load() != 2 {
		t.Fatalf("connections opened = %d", connector.connects.Load())
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
	if kind := retryDeletion(t, store); kind != memory.DeletionDelete {
		t.Fatalf("retry = %s", kind)
	}
	assertDeletedOnce(t, store, len(doomedVersions))
}

// cancellingDeletion cancels the request's context after the writes, before
// the commit.
type cancellingDeletion struct {
	deletion.Store
	cancel context.CancelFunc
}

func (c cancellingDeletion) WithinRecordDeletion(ctx context.Context, operation func(deletion.Transaction) error) error {
	return c.Store.WithinRecordDeletion(ctx, func(tx deletion.Transaction) error {
		return operation(cancellingTransaction{Transaction: tx, cancel: c.cancel})
	})
}

type cancellingTransaction struct {
	deletion.Transaction
	cancel context.CancelFunc
}

func (c cancellingTransaction) DeleteRecordContent(ctx context.Context, d deletion.Deletion) error {
	err := c.Transaction.DeleteRecordContent(ctx, d)
	c.cancel()
	return err
}

// Cancellation that arrives after the writes does not stop the commit: once
// the operation succeeded the commit runs uncancelled, as in the migration, so
// an acknowledged commit is never reported as a failure. The result is
// therefore the truthful delete, and a retry is a no-op.
func TestDeletionCancelledAfterWritesCommits(t *testing.T) {
	store, _ := openFaultyDoomed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got, err := deletion.Service{Store: cancellingDeletion{Store: store.MemoryDeletion(), cancel: cancel}}.DeleteRecord(ctx, doomedRequest())
	if ctx.Err() == nil {
		t.Fatal("context was not cancelled before the commit")
	}
	if err != nil || got.Kind != memory.DeletionDelete || len(got.Receipts) != len(doomedVersions) {
		t.Fatalf("result %+v, error %v", got, err)
	}
	assertPoolOutsideTransaction(t, store)
	if kind := retryDeletion(t, store); kind != memory.DeletionAlreadyDeleted {
		t.Fatalf("retry = %s", kind)
	}
	assertDeletedOnce(t, store, len(doomedVersions))
}

// A record with more versions than one IN list holds is cleared in chunks
// within the one transaction.
func TestDeleteRecordWithMoreVersionsThanOneChunk(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	versions := memoryIDChunk + 44
	fixture := newMemoryFixture("doomed", "project")
	fixture.record.CurrentVersionID = fmt.Sprintf("doomed-v%03d", versions-1)
	err := inTransaction(ctx, store.db, func(tx database) error {
		if err := insertRecord(ctx, tx, fixture.record); err != nil {
			return err
		}
		for index := range versions {
			version := fixture.version
			version.ID = fmt.Sprintf("doomed-v%03d", index)
			version.ContentHash = "sha256:" + version.ID
			content := "версия " + leakMarker + " " + version.ID
			version.Content, version.SearchText = &content, optional(normalizeSearchText(content))
			if err := insertVersion(ctx, tx, version); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, store, "UPDATE memory_records SET version = 3 WHERE id = 'doomed'")
	got := deleteDoomed(t, store, nil)
	if got.Kind != memory.DeletionDelete || len(got.Receipts) != versions || got.RecordVersion != 4 {
		t.Fatalf("result: %s, %d receipts, version %d", got.Kind, len(got.Receipts), got.RecordVersion)
	}
	assertDeletedOnce(t, store, versions)
	if got := queryStrings(t, store, "SELECT CAST(count(*) AS TEXT) FROM memory_record_versions WHERE content LIKE ? OR search_text LIKE ?", "%"+leakMarker+"%", "%"+leakMarker+"%"); !slices.Equal(got, []string{"0"}) {
		t.Fatalf("versions with text = %v", got)
	}
}

// A record with only some versions tombstoned (which one deletion never
// leaves) gets receipts for the remaining versions only, one version bump,
// and loses the sources and review reasons of every version.
func TestDeletePartiallyDeletedRecord(t *testing.T) {
	store := openMemoryStore(t)
	hashes := seedDoomed(t, store)
	mustExec(t, store, "UPDATE memory_record_versions SET content = NULL, search_text = NULL, content_deleted_at = ? WHERE id = 'doomed-v1'", memoryTime)
	got := deleteDoomed(t, store, nil)
	if got.Kind != memory.DeletionDelete || got.RecordVersion != 4 || len(got.Receipts) != 2 {
		t.Fatalf("result = %+v", got)
	}
	if got := queryStrings(t, store, "SELECT subject_id || ':' || content_hash FROM context_deletion_receipts ORDER BY subject_id"); !slices.Equal(got, []string{
		"doomed-v2:" + hashes["doomed-v2"], "doomed-v3:" + hashes["doomed-v3"],
	}) {
		t.Fatalf("receipts = %v", got)
	}
	if got := queryStrings(t, store, "SELECT id || ':' || content_deleted_at FROM memory_record_versions WHERE record_id = 'doomed' ORDER BY id"); !slices.Equal(got, []string{
		"doomed-v1:" + memoryTime, "doomed-v2:" + formatStoredTime(deletionNow), "doomed-v3:" + formatStoredTime(deletionNow),
	}) {
		t.Fatalf("tombstones = %v", got)
	}
	if got := queryStrings(t, store, "SELECT id FROM memory_sources ORDER BY id"); !slices.Equal(got, []string{"keeper-spec"}) {
		t.Fatalf("sources = %v", got)
	}
	if got := queryStrings(t, store, "SELECT id FROM memory_reviews WHERE reason IS NOT NULL"); !slices.Equal(got, []string{"keeper-reject"}) {
		t.Fatalf("reviews with reasons = %v", got)
	}
	if got := queryStrings(t, store, "SELECT CAST(version AS TEXT) FROM memory_records WHERE id = 'doomed'"); !slices.Equal(got, []string{"4"}) {
		t.Fatalf("record version = %v", got)
	}
}

const childDeletePathVariable = "CONDUIT_TURSO_CHILD_DELETE_PATH"

// TestChildProcessDeleteRecord is the deleting child: it opens with the
// multiprocess option, waits at the start barrier, deletes the doomed record
// and reports one CHILD-DELETE line.
func TestChildProcessDeleteRecord(t *testing.T) {
	path := os.Getenv(childDeletePathVariable)
	if path == "" {
		t.Skip("child process helper")
	}
	ctx := context.Background()
	store, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		fmt.Printf("CHILD-DELETE failed open: %v\n", err)
	}
	if store != nil {
		defer store.Close()
	}
	if err := childprocess.AwaitBegin(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := childprocess.Signal(childReported); err != nil {
			t.Error(err)
		}
		if err := childprocess.AwaitRelease(); err != nil {
			t.Error(err)
		}
	}()
	if store == nil {
		return
	}
	got, err := deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(ctx, doomedRequest())
	if err != nil {
		fmt.Printf("CHILD-DELETE failed delete: %v\n", err)
		return
	}
	fmt.Printf("CHILD-DELETE %s\n", got.Kind)
}

func childDeletion(t *testing.T, child *childprocess.Child) string {
	t.Helper()
	output := child.Finish()
	for line := range strings.SplitSeq(output, "\n") {
		if result, ok := strings.CutPrefix(line, "CHILD-DELETE "); ok {
			return result
		}
	}
	t.Fatalf("child reported nothing:\n%s", output)
	return ""
}

// Two processes with the multiprocess option delete the same record with the
// same expected version: exactly one deletes, the other finds it deleted. The
// coordination file exists before the children start, so they do not race on
// creating it (the race that can abort tursogo v0.8.3).
func TestCrossProcessDeletersDeleteOnce(t *testing.T) {
	requireMultiprocessPlatform(t)
	ctx := context.Background()
	for round := range 3 {
		t.Run(strconv.Itoa(round), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "memory.db")
			seeded, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
			if err != nil {
				t.Fatal(err)
			}
			seedDoomed(t, seeded)
			if err := seeded.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path + "-tshm"); err != nil {
				t.Fatalf("coordination file before the race: %v", err)
			}
			first := childprocess.Start(t, "TestChildProcessDeleteRecord", childDeletePathVariable+"="+path)
			second := childprocess.Start(t, "TestChildProcessDeleteRecord", childDeletePathVariable+"="+path)
			childprocess.BeginTogether(first, second)
			first.Await(childReported)
			second.Await(childReported)
			kinds := []string{childDeletion(t, first), childDeletion(t, second)}
			slices.Sort(kinds)
			if !slices.Equal(kinds, []string{string(memory.DeletionAlreadyDeleted), string(memory.DeletionDelete)}) {
				t.Fatalf("children = %v", kinds)
			}
			store, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			assertDeletedOnce(t, store, len(doomedVersions))
		})
	}
}
