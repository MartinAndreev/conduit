package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// storedTimeLayout is UTC with a fixed-width nanosecond fraction, so the text
// of stored times sorts as the times do (spec 012 "Търсене"). It parses as
// time.RFC3339Nano.
const storedTimeLayout = "2006-01-02T15:04:05.000000000Z"

// formatStoredTime is the one format memory writes for its times.
func formatStoredTime(t time.Time) string {
	return t.UTC().Format(storedTimeLayout)
}

// ErrMemoryDeletionDiverged reports that the deletion's guarded writes did not
// match the facts read in the same transaction: a listed version was already
// deleted or the record's version was not the expected one. The transaction is
// rolled back. Like the reader's errors it carries no stored value.
var ErrMemoryDeletionDiverged = errors.New("memory rows differ from the facts the deletion decided on")

// MemoryDeletion returns the record deletion port.
func (s *Store) MemoryDeletion() deletion.Store { return memoryDeletionStore{db: s.db} }

var _ deletion.Store = memoryDeletionStore{}

type memoryDeletionStore struct{ db *sql.DB }

// WithinRecordDeletion runs operation on a pinned connection inside BEGIN
// IMMEDIATE, so the write lock is held from before the facts are read until
// the commit. As in the migration, a failed COMMIT leaves Turso's transaction
// open, so every path that does not commit rolls back explicitly; the commit
// is not cancelled once operation succeeded.
func (s memoryDeletionStore) WithinRecordDeletion(ctx context.Context, operation func(deletion.Transaction) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open memory deletion connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return errors.Join(fmt.Errorf("begin memory deletion: %w", err), conn.Close())
	}
	if err := operation(memoryDeletionTx{db: conn}); err != nil {
		return errors.Join(err, rollbackAndRelease(conn, "memory deletion"))
	}
	if _, err := conn.ExecContext(context.WithoutCancel(ctx), "COMMIT"); err != nil {
		return errors.Join(fmt.Errorf("commit memory deletion: %w", err), rollbackAndRelease(conn, "memory deletion"))
	}
	return conn.Close()
}

type memoryDeletionTx struct{ db database }

// ReadRecordDeletionFacts reads the record by id in any project, and all its
// versions ordered by id. The rule, not this read, refuses another project.
func (t memoryDeletionTx) ReadRecordDeletionFacts(ctx context.Context, recordID string) (deletion.RecordFacts, error) {
	records, versions := table.MemoryRecords, table.MemoryRecordVersions
	query, args := jet.SELECT(records.ID, records.ProjectID, records.Status, records.CurrentVersionID, records.Version).
		FROM(records).WHERE(records.ID.EQ(jet.String(recordID))).Sql()
	var facts deletion.RecordFacts
	record := &facts.Record
	rows, err := t.db.QueryContext(ctx, query, args...)
	if err != nil {
		return deletion.RecordFacts{}, fmt.Errorf("read memory record: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return deletion.RecordFacts{}, fmt.Errorf("read memory record: %w", err)
		}
		return deletion.RecordFacts{}, nil
	}
	if err := rows.Scan(&record.ID, &record.ProjectID, &record.Status, &record.CurrentVersionID, &record.Version); err != nil {
		return deletion.RecordFacts{}, fmt.Errorf("scan memory record: %w", ErrUndecodableMemoryRow)
	}
	if err := rows.Close(); err != nil {
		return deletion.RecordFacts{}, fmt.Errorf("read memory record: %w", err)
	}
	facts.Found = true
	query, args = jet.SELECT(versions.ID, versions.ContentHash, versions.ContentDeletedAt.IS_NOT_NULL()).
		FROM(versions).WHERE(versions.RecordID.EQ(jet.String(recordID))).ORDER_BY(versions.ID).Sql()
	rows, err = t.db.QueryContext(ctx, query, args...)
	if err != nil {
		return deletion.RecordFacts{}, fmt.Errorf("read memory versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var version memory.VersionDeletionState
		if err := rows.Scan(&version.VersionID, &version.ContentHash, &version.ContentDeleted); err != nil {
			return deletion.RecordFacts{}, fmt.Errorf("scan memory version: %w", ErrUndecodableMemoryRow)
		}
		facts.Versions = append(facts.Versions, version)
	}
	if err := rows.Err(); err != nil {
		return deletion.RecordFacts{}, fmt.Errorf("iterate memory versions: %w", err)
	}
	return facts, nil
}

// DeleteRecordContent writes the deletion. Its guarded statements must change
// exactly the rows the facts named, or it fails with ErrMemoryDeletionDiverged
// and the caller's transaction rolls back.
func (t memoryDeletionTx) DeleteRecordContent(ctx context.Context, d deletion.Deletion) error {
	records, versions, sources, reviews, receipts := table.MemoryRecords, table.MemoryRecordVersions, table.MemorySources, table.MemoryReviews, table.ContextDeletionReceipts
	deletedAt := formatStoredTime(d.DeletedAt)
	recordVersions := jet.SELECT(versions.ID).FROM(versions).WHERE(versions.RecordID.EQ(jet.String(d.RecordID)))

	bump := records.UPDATE(records.Version, records.UpdatedAt).
		SET(records.Version.ADD(jet.Int(1)), jet.String(deletedAt)).
		WHERE(records.ID.EQ(jet.String(d.RecordID)).
			AND(records.ProjectID.EQ(jet.String(d.ProjectID))).
			AND(records.Version.EQ(jet.Int(d.ExpectedVersion))))
	if err := t.execExpecting(ctx, "bump memory record version", bump, 1); err != nil {
		return err
	}
	for chunk := range slices.Chunk(d.Versions, memoryIDChunk) {
		ids := make([]string, len(chunk))
		for index, version := range chunk {
			ids[index] = version.VersionID
		}
		clearContent := versions.UPDATE(versions.Content, versions.SearchText, versions.ContentDeletedAt).
			SET(jet.NULL, jet.NULL, jet.String(deletedAt)).
			WHERE(versions.RecordID.EQ(jet.String(d.RecordID)).
				AND(versions.ProjectID.EQ(jet.String(d.ProjectID))).
				AND(versions.ContentDeletedAt.IS_NULL()).
				AND(versions.ID.IN(stringList(ids)...)))
		if err := t.execExpecting(ctx, "clear memory version content", clearContent, int64(len(chunk))); err != nil {
			return err
		}
	}
	if err := t.exec(ctx, "delete memory sources", sources.DELETE().WHERE(sources.VersionID.IN(recordVersions))); err != nil {
		return err
	}
	clearReasons := reviews.UPDATE(reviews.Reason).SET(jet.NULL).
		WHERE(reviews.Reason.IS_NOT_NULL().AND(reviews.VersionID.IN(recordVersions)))
	if err := t.exec(ctx, "clear memory review reasons", clearReasons); err != nil {
		return err
	}
	for chunk := range slices.Chunk(d.Receipts, memoryIDChunk) {
		insert := receipts.INSERT(receipts.ID, receipts.ProjectID, receipts.SubjectKind, receipts.SubjectID, receipts.ContentHash, receipts.DeletedAt)
		for _, receipt := range chunk {
			insert = insert.VALUES(receipt.ID, receipt.ProjectID, string(receipt.SubjectKind), receipt.SubjectID, receipt.ContentHash, formatStoredTime(receipt.DeletedAt))
		}
		if err := t.exec(ctx, "append deletion receipts", insert); err != nil {
			return err
		}
	}
	return nil
}

func (t memoryDeletionTx) exec(ctx context.Context, action string, statement jet.Statement) error {
	query, args := statement.Sql()
	if _, err := t.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func (t memoryDeletionTx) execExpecting(ctx context.Context, action string, statement jet.Statement, rows int64) error {
	query, args := statement.Sql()
	result, err := t.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: inspect result: %w", action, err)
	}
	if changed != rows {
		return fmt.Errorf("%s: %w", action, ErrMemoryDeletionDiverged)
	}
	return nil
}
