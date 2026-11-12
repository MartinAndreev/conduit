package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// ErrMemoryDeletionDiverged reports that the deletion's guarded writes did not
// match the facts read in the same transaction: a listed version was already
// deleted or the record's version was not the expected one. The transaction is
// rolled back. Like the reader's errors it carries no stored value.
var ErrMemoryDeletionDiverged = errors.New("memory rows differ from the facts the deletion decided on")

// MemoryDeletion returns the record deletion port.
func (s *Store) MemoryDeletion() deletion.Store { return memoryDeletionStore{db: s.db} }

var _ deletion.Store = memoryDeletionStore{}

type memoryDeletionStore struct{ db *sql.DB }

// WithinRecordDeletion runs operation in one memory write transaction
// (withMemoryWrite).
func (s memoryDeletionStore) WithinRecordDeletion(ctx context.Context, operation func(deletion.Transaction) error) error {
	return withMemoryWrite(ctx, s.db, "memory deletion", func(conn *sql.Conn) error {
		return operation(memoryDeletionTx{db: conn})
	})
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
	return execStatement(ctx, t.db, action, statement)
}

func (t memoryDeletionTx) execExpecting(ctx context.Context, action string, statement jet.Statement, rows int64) error {
	return execExpectingRows(ctx, t.db, action, statement, rows, ErrMemoryDeletionDiverged)
}
