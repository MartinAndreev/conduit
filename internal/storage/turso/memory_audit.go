package turso

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/audit"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// AuditMaterial returns the audit material read port.
func (s *Store) AuditMaterial() audit.MaterialReader { return memoryReader{db: s.db} }

// AuditSuggestions returns the agent suggestion read port.
func (s *Store) AuditSuggestions() audit.SuggestionReader { return memoryReader{db: s.db} }

// SuggestionConfirmations returns the suggestion confirmation port.
func (s *Store) SuggestionConfirmations() audit.ConfirmationStore {
	return memorySuggestionStore{db: s.db}
}

var (
	_ audit.MaterialReader    = memoryReader{}
	_ audit.SuggestionReader  = memoryReader{}
	_ audit.ConfirmationStore = memorySuggestionStore{}
)

// ReadAuditMaterial reads one page in one snapshot, in two steps: first the
// position and stored content size of at most Limit+1 records in
// (created_at, id) order, without their content, to choose the page within
// the content budget; then the chosen records with their current versions,
// approval, stored source count and size, and the sources of those within
// memory.MaxSourcesPerItem and memory.MaxSourceBytes. Only the chosen
// records' content leaves the engine, and no record's sources over those
// bounds.
func (r memoryReader) ReadAuditMaterial(ctx context.Context, query audit.MaterialQuery) (audit.MaterialPage, error) {
	if query.Limit <= 0 || query.ContentBudget <= 0 || len(query.Statuses) == 0 {
		return audit.MaterialPage{}, fmt.Errorf("read audit material: page size, content budget and statuses must be given, got %d, %d, %d", query.Limit, query.ContentBudget, len(query.Statuses))
	}
	var page audit.MaterialPage
	err := r.withSnapshot(ctx, "read audit material", func(tx *sql.Tx) error {
		ids, more, err := chooseAuditPage(ctx, tx, query)
		if err != nil {
			return err
		}
		page.More = more
		if len(ids) == 0 {
			return nil
		}
		if page.Records, err = readAuditRecords(ctx, tx, ids); err != nil {
			return err
		}
		// Sources over the per-item bounds are not read: the record stays
		// on the page with its stored count and size, and no sources.
		var versionIDs []string
		for _, stored := range page.Records {
			if stored.SourceCount > 0 && stored.SourceCount <= memory.MaxSourcesPerItem && stored.SourceBytes <= memory.MaxSourceBytes {
				versionIDs = append(versionIDs, stored.Record.CurrentVersionID)
			}
		}
		sources, err := readSources(ctx, tx, versionIDs)
		if err != nil {
			return err
		}
		for index := range page.Records {
			page.Records[index].Sources = sources[page.Records[index].Record.CurrentVersionID]
		}
		return nil
	})
	if err != nil {
		return audit.MaterialPage{}, err
	}
	return page, nil
}

// auditCondition selects the project's records in the query's statuses whose
// current version's content is not deleted, strictly after query.After.
func auditCondition(query audit.MaterialQuery) jet.BoolExpression {
	recordTable, versions := table.MemoryRecords, table.MemoryRecordVersions
	statuses := make([]jet.Expression, len(query.Statuses))
	for index, status := range query.Statuses {
		statuses[index] = jet.String(string(status))
	}
	condition := recordTable.ProjectID.EQ(jet.String(query.ProjectID)).
		AND(recordTable.Status.IN(statuses...)).
		AND(versions.ContentDeletedAt.IS_NULL())
	if query.After.RecordID != "" {
		condition = condition.AND(positionAfter(query.After))
	}
	return condition
}

func currentVersionJoin() jet.ReadableTable {
	recordTable, versions := table.MemoryRecords, table.MemoryRecordVersions
	return recordTable.INNER_JOIN(versions, versions.ID.EQ(recordTable.CurrentVersionID).AND(versions.RecordID.EQ(recordTable.ID)))
}

// chooseAuditPage returns the IDs of the page's records in order, and
// whether more follow: at most query.Limit, stopping before the record that
// would take the total content bytes over query.ContentBudget, but at least
// one when any remains. A page size is at most audit.MaxAuditPageSize, well
// under memoryIDChunk, so the IDs fit one IN list.
func chooseAuditPage(ctx context.Context, tx *sql.Tx, query audit.MaterialQuery) ([]string, bool, error) {
	recordTable, versions := table.MemoryRecords, table.MemoryRecordVersions
	statement, args := jet.SELECT(recordTable.ID, jet.COALESCE(byteLength(versions.Content), jet.Int(0))).
		FROM(currentVersionJoin()).WHERE(auditCondition(query)).
		ORDER_BY(recordTable.CreatedAt, recordTable.ID).LIMIT(int64(query.Limit) + 1).Sql()
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, false, fmt.Errorf("read audit page sizes: %w", err)
	}
	defer rows.Close()
	var ids []string
	more, total := false, int64(0)
	for rows.Next() {
		var id string
		var size int64
		if err := rows.Scan(&id, &size); err != nil {
			return nil, false, fmt.Errorf("scan audit page size: %w", ErrUndecodableMemoryRow)
		}
		if len(ids) == query.Limit || (len(ids) > 0 && total+size > int64(query.ContentBudget)) {
			more = true
			break
		}
		ids, total = append(ids, id), total+size
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate audit page sizes: %w", err)
	}
	return ids, more, nil
}

// readAuditRecords reads the chosen records with their current versions, in
// (created_at, id) order.
func readAuditRecords(ctx context.Context, tx *sql.Tx, ids []string) ([]audit.MaterialRecord, error) {
	recordTable, versions := table.MemoryRecords, table.MemoryRecordVersions
	sourceCount, sourceBytes := sourceSizes()
	statement, args := jet.SELECT(
		recordTable.ID, recordTable.ProjectID, recordTable.Category, recordTable.Status, recordTable.CurrentVersionID, recordTable.Version, recordTable.CreatedAt,
		versions.Content, versions.ContentHash, versions.Trust, exactVersionApproved(), sourceCount, sourceBytes,
	).FROM(currentVersionJoin()).WHERE(recordTable.ID.IN(stringList(ids)...).AND(versions.ContentDeletedAt.IS_NULL())).
		ORDER_BY(recordTable.CreatedAt, recordTable.ID).Sql()
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("read audit records: %w", err)
	}
	defer rows.Close()
	var result []audit.MaterialRecord
	for rows.Next() {
		var stored audit.MaterialRecord
		record := &stored.Record
		var createdAt string
		if err := rows.Scan(
			&record.ID, &record.ProjectID, &record.Category, &record.Status, &record.CurrentVersionID, &record.Version, &createdAt,
			&stored.Content, &stored.ContentHash, &stored.Trust, &stored.ExactVersionApproved, &stored.SourceCount, &stored.SourceBytes,
		); err != nil {
			return nil, fmt.Errorf("scan audit record: %w", ErrUndecodableMemoryRow)
		}
		if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, fmt.Errorf("audit record creation time: %w", ErrInvalidStoredTimestamp)
		}
		result = append(result, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit records: %w", err)
	}
	return result, nil
}

// ReadAgentSuggestions reads one page of the reviews carrying a suggestion
// about versions of the project's records, whatever their standing, ordered
// by the text of reviewed_at then id (the writer stores times in UTC with a
// fixed width, so text order is time order), each with its record's current
// state. It reads one row past the page to learn whether more follow.
func (r memoryReader) ReadAgentSuggestions(ctx context.Context, projectID string, after audit.SuggestionPosition, limit int) (audit.SuggestionPage, error) {
	if limit <= 0 {
		return audit.SuggestionPage{}, fmt.Errorf("read agent suggestions: page size must be positive, got %d", limit)
	}
	reviews, named := table.MemoryReviews, table.MemoryRecordVersions.AS("named")
	condition := named.ProjectID.EQ(jet.String(projectID)).AND(reviews.Suggestion.IS_NOT_NULL())
	if after.ReviewID != "" {
		afterTime := jet.String(formatStoredTime(after.ReviewedAt))
		condition = condition.AND(reviews.ReviewedAt.GT(afterTime).
			OR(reviews.ReviewedAt.EQ(afterTime).AND(reviews.ID.GT(jet.String(after.ReviewID)))))
	}
	var page audit.SuggestionPage
	err := r.withSnapshot(ctx, "read agent suggestions", func(tx *sql.Tx) error {
		suggestions, err := readSuggestions(ctx, tx, condition, limit+1)
		if err != nil {
			return err
		}
		if len(suggestions) > limit {
			page.More, suggestions = true, suggestions[:limit]
		}
		page.Suggestions = suggestions
		return nil
	})
	if err != nil {
		return audit.SuggestionPage{}, err
	}
	return page, nil
}

// readSuggestions reads the reviews matching condition, in (reviewed_at, id)
// order, with the record of the version each names and that record's current
// version. The review's version is aliased "named" in condition.
func readSuggestions(ctx context.Context, db database, condition jet.BoolExpression, limit int) ([]audit.StoredSuggestion, error) {
	reviews, recordTable, versions := table.MemoryReviews, table.MemoryRecords, table.MemoryRecordVersions
	named := versions.AS("named")
	statement, args := jet.SELECT(
		reviews.ID, reviews.Reviewer, jet.COALESCE(reviews.Reason, jet.String("")), reviews.ReviewedAt,
		reviews.Actor, reviews.Decision, jet.COALESCE(reviews.Suggestion, jet.String("")), reviews.VersionID, reviews.ContentHash,
		recordTable.ID, recordTable.ProjectID, recordTable.Category, recordTable.Status, recordTable.CurrentVersionID, recordTable.Version, recordTable.CreatedAt,
		versions.ContentHash, versions.ContentDeletedAt.IS_NOT_NULL(),
	).FROM(
		reviews.
			INNER_JOIN(named, named.ID.EQ(reviews.VersionID)).
			INNER_JOIN(recordTable, recordTable.ID.EQ(named.RecordID)).
			INNER_JOIN(versions, versions.ID.EQ(recordTable.CurrentVersionID).AND(versions.RecordID.EQ(recordTable.ID))),
	).WHERE(condition).ORDER_BY(reviews.ReviewedAt, reviews.ID).LIMIT(int64(limit)).Sql()
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("read memory suggestions: %w", err)
	}
	defer rows.Close()
	var result []audit.StoredSuggestion
	for rows.Next() {
		var stored audit.StoredSuggestion
		standing, record := &stored.Standing, &stored.Standing.Record
		var reviewedAt, createdAt string
		if err := rows.Scan(
			&stored.ReviewID, &stored.Reviewer, &stored.Reason, &reviewedAt,
			&standing.Actor, &standing.Decision, &standing.Suggestion, &standing.VersionID, &standing.ContentHash,
			&record.ID, &record.ProjectID, &record.Category, &record.Status, &record.CurrentVersionID, &record.Version, &createdAt,
			&standing.CurrentContentHash, &standing.CurrentContentDeleted,
		); err != nil {
			return nil, fmt.Errorf("scan memory suggestion: %w", ErrUndecodableMemoryRow)
		}
		if stored.ReviewedAt, err = time.Parse(time.RFC3339Nano, reviewedAt); err != nil {
			return nil, fmt.Errorf("memory suggestion review time: %w", ErrInvalidStoredTimestamp)
		}
		if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, fmt.Errorf("memory record creation time: %w", ErrInvalidStoredTimestamp)
		}
		standing.ReviewFound, standing.RecordFound = true, true
		result = append(result, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory suggestions: %w", err)
	}
	return result, nil
}

type memorySuggestionStore struct{ db *sql.DB }

// WithinSuggestionConfirmation runs operation in one memory write
// transaction (withMemoryWrite) whose record write and deletion statements
// share the pinned connection.
func (s memorySuggestionStore) WithinSuggestionConfirmation(ctx context.Context, operation func(audit.ConfirmationTransaction) error) error {
	return withMemoryWrite(ctx, s.db, "memory suggestion confirmation", func(conn *sql.Conn) error {
		return operation(memorySuggestionTx{memoryRecordTx: memoryRecordTx{db: conn}, memoryDeletionTx: memoryDeletionTx{db: conn}})
	})
}

// memorySuggestionTx is the record write and deletion transactions on one
// connection, with the suggestion read.
type memorySuggestionTx struct {
	memoryRecordTx
	memoryDeletionTx
}

// ReadSuggestion reads the review by id in any project; the rule, not this
// read, refuses another project or a review without a suggestion.
func (t memorySuggestionTx) ReadSuggestion(ctx context.Context, reviewID string) (audit.StoredSuggestion, error) {
	suggestions, err := readSuggestions(ctx, t.memoryRecordTx.db, table.MemoryReviews.ID.EQ(jet.String(reviewID)), 1)
	if err != nil || len(suggestions) == 0 {
		return audit.StoredSuggestion{}, err
	}
	return suggestions[0], nil
}
