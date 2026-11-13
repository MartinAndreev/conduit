package turso

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/proposals"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// searchTextIdentity identifies a search text normalized by version: the
// version and the SHA-256 of the text. The digest is computed here, so the
// text never leaves storage for a comparison.
func searchTextIdentity(searchText string, version int64) memory.SearchTextIdentity {
	sum := sha256.Sum256([]byte(searchText))
	return memory.SearchTextIdentity{NormalizationVersion: version, Digest: hex.EncodeToString(sum[:])}
}

// SearchNormalizer returns the normalization InsertRecord applies, as the
// identity of the search text it would write.
func (s *Store) SearchNormalizer() proposals.SearchNormalizer { return searchNormalizer{} }

var _ proposals.SearchNormalizer = searchNormalizer{}

type searchNormalizer struct{}

func (searchNormalizer) SearchTextIdentity(content string) memory.SearchTextIdentity {
	return searchTextIdentity(normalizeSearchText(content), searchNormalizationVersion)
}

// ProposalQueue returns the review queue read port.
func (s *Store) ProposalQueue() proposals.QueueReader { return memoryReader{db: s.db} }

var _ proposals.QueueReader = memoryReader{}

// ReadReviewQueue reads one page of the project's proposed records whose
// current version's content is not deleted, ordered by the text of the
// record's created_at then id (as the retention sweep), in one snapshot. It
// reads one row past the page to learn whether more follow. Each item's
// search text identity carries the normalization version stored with it.
func (r memoryReader) ReadReviewQueue(ctx context.Context, projectID string, after records.ProposalPosition, limit int) (proposals.QueuePage, error) {
	if limit <= 0 {
		return proposals.QueuePage{}, fmt.Errorf("read review queue: page size must be positive, got %d", limit)
	}
	var page proposals.QueuePage
	err := r.withSnapshot(ctx, "read review queue", func(tx *sql.Tx) error {
		items, err := readQueueRows(ctx, tx, projectID, after, limit+1)
		if err != nil {
			return err
		}
		if len(items) > limit {
			page.More, items = true, items[:limit]
		}
		ids := make([]string, len(items))
		for index, item := range items {
			ids[index] = item.Record.CurrentVersionID
		}
		sources := make(map[string][]memory.RecordSource)
		for chunk := range slices.Chunk(ids, memoryIDChunk) {
			read, err := readSources(ctx, tx, chunk)
			if err != nil {
				return err
			}
			for versionID, versionSources := range read {
				sources[versionID] = versionSources
			}
		}
		for index := range items {
			items[index].Sources = sources[items[index].Record.CurrentVersionID]
		}
		page.Items = items
		return nil
	})
	if err != nil {
		return proposals.QueuePage{}, err
	}
	return page, nil
}

func readQueueRows(ctx context.Context, tx *sql.Tx, projectID string, after records.ProposalPosition, limit int) ([]proposals.QueueItem, error) {
	recordTable, versions := table.MemoryRecords, table.MemoryRecordVersions
	condition := recordTable.ProjectID.EQ(jet.String(projectID)).
		AND(recordTable.Status.EQ(jet.String(string(memory.StatusProposed)))).
		AND(versions.ContentDeletedAt.IS_NULL())
	if after.RecordID != "" {
		condition = condition.AND(positionAfter(after))
	}
	query, args := jet.SELECT(
		recordTable.ID, recordTable.ProjectID, recordTable.Category, recordTable.Status, recordTable.CurrentVersionID, recordTable.Version, recordTable.CreatedAt,
		versions.Content, versions.ContentHash, versions.SearchText, versions.SearchNormalizationVersion, versions.Trust,
		versions.Visibility, versions.Origin, versions.RunID, versions.AttemptID, versions.BaseCommit, versions.AcceptedResultRef,
	).FROM(
		recordTable.INNER_JOIN(versions, versions.ID.EQ(recordTable.CurrentVersionID).AND(versions.RecordID.EQ(recordTable.ID))),
	).WHERE(condition).ORDER_BY(recordTable.CreatedAt, recordTable.ID).LIMIT(int64(limit)).Sql()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read review queue rows: %w", err)
	}
	defer rows.Close()
	var items []proposals.QueueItem
	for rows.Next() {
		item, err := scanQueueRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review queue rows: %w", err)
	}
	return items, nil
}

func scanQueueRow(rows *sql.Rows) (proposals.QueueItem, error) {
	var item proposals.QueueItem
	record, scope := &item.Record, &item.Scope
	var createdAt, searchText string
	var normalizationVersion int64
	var runID, attemptID, baseCommit, acceptedResultRef sql.NullString
	if err := rows.Scan(
		&record.ID, &record.ProjectID, &record.Category, &record.Status, &record.CurrentVersionID, &record.Version, &createdAt,
		&item.Content, &item.ContentHash, &searchText, &normalizationVersion, &item.Trust,
		&scope.Visibility, &scope.Origin, &runID, &attemptID, &baseCommit, &acceptedResultRef,
	); err != nil {
		return proposals.QueueItem{}, fmt.Errorf("scan review queue row: %w", ErrUndecodableMemoryRow)
	}
	var err error
	if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return proposals.QueueItem{}, fmt.Errorf("review queue creation time: %w", ErrInvalidStoredTimestamp)
	}
	scope.ProjectID = record.ProjectID
	scope.RunID, scope.AttemptID, scope.BaseCommit, scope.AcceptedResultRef = runID.String, attemptID.String, baseCommit.String, acceptedResultRef.String
	item.SearchText = searchTextIdentity(searchText, normalizationVersion)
	return item, nil
}

// positionAfter selects records strictly after a position in (created_at,
// id) order, comparing the text of created_at, which the writer stores in
// UTC with a fixed width, so a position's time formats back to the stored
// text.
func positionAfter(after records.ProposalPosition) jet.BoolExpression {
	recordTable := table.MemoryRecords
	afterTime := jet.String(formatStoredTime(after.CreatedAt))
	return recordTable.CreatedAt.GT(afterTime).
		OR(recordTable.CreatedAt.EQ(afterTime).AND(recordTable.ID.GT(jet.String(after.RecordID))))
}
