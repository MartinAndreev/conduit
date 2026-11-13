package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/id"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/model"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// ErrMemoryWriteDiverged reports that a record write's guarded statement did
// not match the facts read in the same transaction: the record was not at the
// expected version or status, the copied version was deleted or not the
// record's, or a review named a missing or deleted version. The transaction is
// rolled back. Like the reader's errors it carries no stored value.
var ErrMemoryWriteDiverged = errors.New("memory rows differ from the facts the write decided on")

// MemoryRecords returns the record write port.
func (s *Store) MemoryRecords() records.Store { return memoryRecordStore{db: s.db} }

var _ records.Store = memoryRecordStore{}

type memoryRecordStore struct{ db *sql.DB }

// WithinRecordWrite runs operation in one memory write transaction
// (withMemoryWrite).
func (s memoryRecordStore) WithinRecordWrite(ctx context.Context, operation func(records.Transaction) error) error {
	return withMemoryWrite(ctx, s.db, "memory record write", func(conn *sql.Conn) error {
		return operation(memoryRecordTx{db: conn})
	})
}

type memoryRecordTx struct{ db database }

// ReadRecord reads the record by id in any project with its current version
// and whether a human approved or rejected any of its versions, then the
// current version's sources ordered by id. The rules, not this read, refuse
// another project.
func (t memoryRecordTx) ReadRecord(ctx context.Context, recordID string) (records.RecordFacts, error) {
	recordTable, versions, reviews := table.MemoryRecords, table.MemoryRecordVersions, table.MemoryReviews
	decided := versions.AS("decided")
	humanDecision := jet.EXISTS(
		jet.SELECT(jet.Int(1)).FROM(reviews.INNER_JOIN(decided, decided.ID.EQ(reviews.VersionID))).WHERE(
			decided.RecordID.EQ(recordTable.ID).
				AND(reviews.Actor.EQ(jet.String(string(memory.ActorHuman)))).
				AND(reviews.Decision.IN(jet.String(string(memory.ReviewApprove)), jet.String(string(memory.ReviewReject)))),
		),
	)
	query, args := jet.SELECT(
		recordTable.ID, recordTable.ProjectID, recordTable.Category, recordTable.Status, recordTable.CurrentVersionID, recordTable.Version, recordTable.CreatedAt,
		versions.ContentHash, versions.ContentDeletedAt.IS_NOT_NULL(), humanDecision,
	).FROM(
		recordTable.INNER_JOIN(versions, versions.ID.EQ(recordTable.CurrentVersionID).AND(versions.RecordID.EQ(recordTable.ID))),
	).WHERE(recordTable.ID.EQ(jet.String(recordID))).Sql()
	var facts records.RecordFacts
	record := &facts.Record
	var createdAt string
	found, err := scanOne(ctx, t.db, "read memory record", query, args,
		&record.ID, &record.ProjectID, &record.Category, &record.Status, &record.CurrentVersionID, &record.Version, &createdAt,
		&facts.CurrentContentHash, &facts.CurrentContentDeleted, &facts.HumanDecisionRecorded,
	)
	if err != nil || !found {
		return records.RecordFacts{}, err
	}
	if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return records.RecordFacts{}, fmt.Errorf("memory record creation time: %w", ErrInvalidStoredTimestamp)
	}
	facts.Found = true
	sources, err := readSources(ctx, t.db, []string{record.CurrentVersionID})
	if err != nil {
		return records.RecordFacts{}, err
	}
	facts.Sources = sources[record.CurrentVersionID]
	return facts, nil
}

// scanOne scans the query's first row into destinations and reports whether
// there was one. A value that does not fit is ErrUndecodableMemoryRow, whose
// message carries no stored value.
func scanOne(ctx context.Context, db database, action, query string, args []any, destinations ...any) (bool, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("%s: %w", action, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("%s: %w", action, err)
		}
		return false, nil
	}
	if err := rows.Scan(destinations...); err != nil {
		return false, fmt.Errorf("%s: %w", action, ErrUndecodableMemoryRow)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("%s: %w", action, err)
	}
	return true, nil
}

// ReadVersion reads one version of the record.
func (t memoryRecordTx) ReadVersion(ctx context.Context, recordID, versionID string) (records.VersionFacts, error) {
	versions := table.MemoryRecordVersions
	query, args := jet.SELECT(versions.ContentHash, versions.ContentDeletedAt.IS_NOT_NULL()).FROM(versions).
		WHERE(versions.ID.EQ(jet.String(versionID)).AND(versions.RecordID.EQ(jet.String(recordID)))).Sql()
	var facts records.VersionFacts
	found, err := scanOne(ctx, t.db, "read memory version", query, args, &facts.ContentHash, &facts.ContentDeleted)
	if err != nil || !found {
		return records.VersionFacts{}, err
	}
	facts.Found = true
	return facts, nil
}

// ReadExpiredProposals selects and orders by the text of created_at, which
// the writer stores in UTC with a fixed width, so text order is time order
// (positionAfter).
func (t memoryRecordTx) ReadExpiredProposals(ctx context.Context, cutoff time.Time, after records.ProposalPosition, limit int) ([]records.ProposalPosition, error) {
	recordTable := table.MemoryRecords
	condition := recordTable.Status.EQ(jet.String(string(memory.StatusProposed))).
		AND(recordTable.CreatedAt.LT_EQ(jet.String(formatStoredTime(cutoff))))
	if after.RecordID != "" {
		condition = condition.AND(positionAfter(after))
	}
	query, args := jet.SELECT(recordTable.CreatedAt, recordTable.ID).FROM(recordTable).WHERE(condition).
		ORDER_BY(recordTable.CreatedAt, recordTable.ID).LIMIT(int64(limit)).Sql()
	rows, err := t.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read expired proposals: %w", err)
	}
	defer rows.Close()
	var positions []records.ProposalPosition
	for rows.Next() {
		var position records.ProposalPosition
		var createdAt string
		if err := rows.Scan(&createdAt, &position.RecordID); err != nil {
			return nil, fmt.Errorf("scan expired proposal: %w", ErrUndecodableMemoryRow)
		}
		if position.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, fmt.Errorf("expired proposal creation time: %w", ErrInvalidStoredTimestamp)
		}
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired proposals: %w", err)
	}
	return positions, nil
}

// SearchTextBytes measures the search text InsertRecord writes for content.
func (memoryRecordTx) SearchTextBytes(content string) int64 {
	return int64(len(normalizeSearchText(content)))
}

// InsertRecord writes the record before its version: the version's foreign
// key to the record is immediate, the record's to its current version is
// deferred to the commit.
func (t memoryRecordTx) InsertRecord(ctx context.Context, created records.NewRecord) error {
	recordTable, versions := table.MemoryRecords, table.MemoryRecordVersions
	createdAt := formatStoredTime(created.CreatedAt)
	record := created.Record
	insertRecord := recordTable.INSERT(recordTable.AllColumns).MODEL(model.MemoryRecords{
		ID: record.ID, ProjectID: record.ProjectID, Category: string(record.Category), Status: string(record.Status),
		CurrentVersionID: record.CurrentVersionID, Version: record.Version, CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	if err := execStatement(ctx, t.db, "insert memory record", insertRecord); err != nil {
		return err
	}
	version, scope := created.Version, created.Version.Scope
	searchText := normalizeSearchText(version.Content)
	insertVersion := versions.INSERT(versions.AllColumns).MODEL(model.MemoryRecordVersions{
		ID: version.ID, RecordID: record.ID, ProjectID: record.ProjectID,
		Content: &version.Content, ContentHash: version.ContentHash,
		SearchText: &searchText, SearchNormalizationVersion: searchNormalizationVersion,
		Author: version.Author, Trust: string(version.Trust), RedactionPolicyVersion: version.RedactionPolicyVersion,
		Visibility: string(scope.Visibility), Origin: string(scope.Origin),
		RunID: present(scope.RunID), AttemptID: present(scope.AttemptID), BaseCommit: present(scope.BaseCommit), AcceptedResultRef: present(scope.AcceptedResultRef),
		CreatedAt: createdAt,
	})
	if err := execStatement(ctx, t.db, "insert memory version", insertVersion); err != nil {
		return err
	}
	return t.insertSources(ctx, version.ID, created.Sources)
}

// insertSources writes a version's sources with adapter-local IDs.
func (t memoryRecordTx) insertSources(ctx context.Context, versionID string, sources []memory.RecordSource) error {
	if len(sources) == 0 {
		return nil
	}
	sourceTable := table.MemorySources
	rows := make([]model.MemorySources, len(sources))
	for index, source := range sources {
		sourceID, err := id.New("source")
		if err != nil {
			return err
		}
		rows[index] = model.MemorySources{
			ID: sourceID, VersionID: versionID, Kind: string(source.Kind),
			Ref: present(source.Ref), Path: present(source.Path), BlobHash: present(source.BlobHash),
		}
	}
	return execStatement(ctx, t.db, "insert memory sources", sourceTable.INSERT(sourceTable.AllColumns).MODELS(rows))
}

// present stores an absent optional value as NULL, never ”.
func present(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ChangeStatus is guarded by the project, the expected version and the
// status the decision was about.
func (t memoryRecordTx) ChangeStatus(ctx context.Context, change records.StatusChange) error {
	recordTable := table.MemoryRecords
	columns := jet.ColumnList{recordTable.Status, recordTable.Version, recordTable.UpdatedAt}
	values := []any{jet.String(string(change.To)), recordTable.Version.ADD(jet.Int(1)), jet.String(formatStoredTime(change.UpdatedAt))}
	if change.CurrentVersionID != "" {
		columns = append(columns, recordTable.CurrentVersionID)
		values = append(values, jet.String(change.CurrentVersionID))
	}
	update := recordTable.UPDATE(columns).SET(values[0], values[1:]...).WHERE(
		recordTable.ID.EQ(jet.String(change.RecordID)).
			AND(recordTable.ProjectID.EQ(jet.String(change.ProjectID))).
			AND(recordTable.Version.EQ(jet.Int(change.ExpectedVersion))).
			AND(recordTable.Status.EQ(jet.String(string(change.From)))),
	)
	return execExpectingRows(ctx, t.db, "change memory record status", update, 1, ErrMemoryWriteDiverged)
}

// CopyVersion copies the stored row inside the engine, so the content never
// leaves it, and refuses a deleted or foreign source version.
func (t memoryRecordTx) CopyVersion(ctx context.Context, copied records.VersionCopy) error {
	versions := table.MemoryRecordVersions
	columns := jet.ColumnList{
		versions.ID, versions.RecordID, versions.ProjectID, versions.Content, versions.ContentHash, versions.SearchText, versions.SearchNormalizationVersion,
		versions.Author, versions.Trust, versions.RedactionPolicyVersion, versions.Visibility, versions.Origin,
		versions.RunID, versions.AttemptID, versions.BaseCommit, versions.AcceptedResultRef, versions.CreatedAt,
	}
	source := jet.SELECT(
		jet.String(copied.VersionID), versions.RecordID, versions.ProjectID, versions.Content, versions.ContentHash, versions.SearchText, versions.SearchNormalizationVersion,
		versions.Author, versions.Trust, versions.RedactionPolicyVersion, versions.Visibility, versions.Origin,
		versions.RunID, versions.AttemptID, versions.BaseCommit, versions.AcceptedResultRef, jet.String(formatStoredTime(copied.CreatedAt)),
	).FROM(versions).WHERE(
		versions.ID.EQ(jet.String(copied.FromVersionID)).
			AND(versions.RecordID.EQ(jet.String(copied.RecordID))).
			AND(versions.ProjectID.EQ(jet.String(copied.ProjectID))).
			AND(versions.ContentDeletedAt.IS_NULL()),
	)
	if err := execExpectingRows(ctx, t.db, "copy memory version", versions.INSERT(columns).QUERY(source), 1, ErrMemoryWriteDiverged); err != nil {
		return err
	}
	return t.insertSources(ctx, copied.VersionID, copied.Sources)
}

// InsertReview selects the named version and content hash, so a review of a
// missing version writes nothing, and a review with a reason is refused on a
// version whose content was deleted (spec 012 "Задължение на пътищата за
// запис").
func (t memoryRecordTx) InsertReview(ctx context.Context, review records.Review) error {
	reviews, versions := table.MemoryReviews, table.MemoryRecordVersions
	columns := jet.ColumnList{reviews.ID, reviews.VersionID, reviews.ContentHash, reviews.Decision, reviews.Actor, reviews.Reviewer, reviews.ReviewedAt}
	projections := []jet.Projection{
		jet.String(review.ID), versions.ID, versions.ContentHash, jet.String(string(review.Decision)), jet.String(string(review.Actor)),
		jet.String(review.Reviewer), jet.String(formatStoredTime(review.ReviewedAt)),
	}
	named := versions.ID.EQ(jet.String(review.VersionID)).AND(versions.ContentHash.EQ(jet.String(review.ContentHash)))
	if review.Reason != "" {
		columns = append(columns, reviews.Reason)
		projections = append(projections, jet.String(review.Reason))
		named = named.AND(versions.ContentDeletedAt.IS_NULL())
	}
	insert := reviews.INSERT(columns).QUERY(jet.SELECT(projections[0], projections[1:]...).FROM(versions).WHERE(named))
	return execExpectingRows(ctx, t.db, "insert memory review", insert, 1, ErrMemoryWriteDiverged)
}

func (t memoryRecordTx) InsertRelation(ctx context.Context, relation records.Relation) error {
	relations := table.MemoryRelations
	insert := relations.INSERT(relations.AllColumns).MODEL(model.MemoryRelations{
		ProjectID: relation.ProjectID, FromRecordID: relation.FromRecordID, ToRecordID: relation.ToRecordID,
		Kind: string(relation.Kind), CreatedAt: formatStoredTime(relation.CreatedAt),
	})
	return execStatement(ctx, t.db, "insert memory relation", insert)
}
