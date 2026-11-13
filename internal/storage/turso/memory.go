package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// maxSearchTerms bounds the distinct normalized terms of one query. Each term
// is one LIKE per scanned row; terms after the first 16 distinct ones are
// ignored and not counted in QueryTerms.
const maxSearchTerms = 16

// maxSearchQueryBytes bounds how much of the query text is read for terms.
// The query is not rejected above it: like terms after the 16th, the rest is
// ignored, so a long assignment text still searches by its beginning.
const maxSearchQueryBytes = 4 * 1024

// memoryIDChunk bounds the IDs bound into one IN list, well under the engine's
// parameter limit even when a query binds the list twice.
const memoryIDChunk = 256

// Reader errors reach composition results and logs, so they never carry a
// stored ID or value: a stored ID may itself be secret-shaped. They are
// classified by these sentinels instead.
var (
	// ErrSearchNormalizationMismatch reports that the scanned window holds a
	// current version whose search text was normalized by another version of
	// normalizeSearchText. Such text cannot be compared with this query, so
	// the search fails instead of silently missing or matching it.
	ErrSearchNormalizationMismatch = errors.New("memory search text was normalized by another version")
	// ErrInvalidStoredTimestamp reports a stored memory time that does not
	// parse as RFC 3339.
	ErrInvalidStoredTimestamp = errors.New("stored memory time is not a valid timestamp")
	// ErrUndecodableMemoryRow reports a stored memory row whose values do not
	// fit the reader's types.
	ErrUndecodableMemoryRow = errors.New("stored memory row cannot be decoded")
)

// MemoryReader returns the memory read port. Every call reads one consistent
// snapshot inside a transaction that is rolled back.
func (s *Store) MemoryReader() compose.MemoryReader { return memoryReader{db: s.db} }

var _ compose.MemoryReader = memoryReader{}

type memoryReader struct{ db *sql.DB }

// ReadLinkedRecords returns the requested records of the project with their
// current versions, in request order without duplicates. Missing IDs are
// absent. No lifecycle, visibility or trust filter is applied.
func (r memoryReader) ReadLinkedRecords(ctx context.Context, projectID string, recordIDs []string) ([]compose.Candidate, error) {
	ids := distinct(recordIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var result []compose.Candidate
	err := r.withSnapshot(ctx, "read linked memory records", func(tx *sql.Tx) error {
		byID := make(map[string]compose.Candidate, len(ids))
		for chunk := range slices.Chunk(ids, memoryIDChunk) {
			candidates, err := readLinkedChunk(ctx, tx, projectID, chunk)
			if err != nil {
				return err
			}
			for _, candidate := range candidates {
				byID[candidate.Record.ID] = candidate
			}
		}
		for _, id := range ids {
			if candidate, ok := byID[id]; ok {
				result = append(result, candidate)
			}
		}
		return attachFacts(ctx, tx, projectID, result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SearchRecords scans the newest ScanLimit current versions of the project
// whose content is not deleted, ordered by version created_at then id, both
// descending. A hit is a scanned version whose search text contains at least
// one distinct normalized query term as a literal substring. Hits keep the
// scan order. LimitReached means the project has more such versions than the
// scan saw. A query without terms returns QueryTerms 0 and no hits.
func (r memoryReader) SearchRecords(ctx context.Context, query compose.SearchQuery) (compose.SearchResult, error) {
	if query.ScanLimit <= 0 {
		return compose.SearchResult{}, fmt.Errorf("search memory records: scan limit must be positive, got %d", query.ScanLimit)
	}
	terms := searchTerms(query.Text)
	result := compose.SearchResult{QueryTerms: len(terms)}
	if len(terms) == 0 {
		return result, nil
	}
	err := r.withSnapshot(ctx, "search memory records", func(tx *sql.Tx) error {
		hits, limitReached, err := scanSearchWindow(ctx, tx, query.ProjectID, terms, query.ScanLimit)
		if err != nil {
			return err
		}
		candidates := make([]compose.Candidate, len(hits))
		for index, hit := range hits {
			candidates[index] = hit.Candidate
		}
		if err := attachFacts(ctx, tx, query.ProjectID, candidates); err != nil {
			return err
		}
		for index := range hits {
			hits[index].Candidate = candidates[index]
		}
		result.Hits, result.LimitReached = hits, limitReached
		return nil
	})
	if err != nil {
		return compose.SearchResult{}, err
	}
	return result, nil
}

// withSnapshot runs read inside a transaction that is always rolled back, so
// the several queries of one call see one state.
func (r memoryReader) withSnapshot(ctx context.Context, action string, read func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin read: %w", action, err)
	}
	if err := read(tx); err != nil {
		return errors.Join(fmt.Errorf("%s: %w", action, err), rollbackRead(tx))
	}
	if err := rollbackRead(tx); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func rollbackRead(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("end read: %w", err)
	}
	return nil
}

// searchTerms returns the query's distinct whitespace-separated terms,
// normalized as search_text was written, in query order, at most
// maxSearchTerms. Only the first maxSearchQueryBytes of the query are read; a
// term cut by that bound is dropped rather than searched in part. Normalizing
// term by term equals normalizing the whole text: Unicode lowercase never
// turns a space into a letter or back.
func searchTerms(text string) []string {
	if len(text) > maxSearchQueryBytes {
		next, _ := utf8.DecodeRuneInString(text[maxSearchQueryBytes:])
		text = text[:maxSearchQueryBytes]
		if !unicode.IsSpace(next) {
			text = text[:strings.LastIndexFunc(text, unicode.IsSpace)+1]
		}
	}
	terms := make([]string, 0, maxSearchTerms)
	seen := make(map[string]bool, maxSearchTerms)
	for field := range strings.FieldsSeq(text) {
		term := normalizeSearchText(field)
		if seen[term] {
			continue
		}
		seen[term] = true
		if terms = append(terms, term); len(terms) == maxSearchTerms {
			break
		}
	}
	return terms
}

// distinct keeps the first occurrence of each value, in order.
func distinct(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// likeContains is a LIKE pattern matching term as a literal substring under
// ESCAPE '\'.
func likeContains(term string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
	return "%" + escaped + "%"
}

func likeEscaped(column jet.StringExpression, pattern string) jet.BoolExpression {
	return jet.BoolExp(jet.CustomExpression(column, jet.Token("LIKE"), jet.String(pattern), jet.Token(`ESCAPE '\'`)))
}

// scanSearchWindow selects the newest scanLimit+1 current non-deleted versions
// of the project. The first scanLimit form the window; one more proves the
// limit was reached. Only hits, incomparable rows and that extra row leave the
// engine, each with one match flag per term and its candidate columns.
func scanSearchWindow(ctx context.Context, tx *sql.Tx, projectID string, terms []string, scanLimit int) ([]compose.SearchHit, bool, error) {
	records, versions := table.MemoryRecords, table.MemoryRecordVersions
	window := jet.CTE("memory_search_window")
	position := jet.IntegerColumn("position").From(window)
	windowVersion := versions.ID.From(window)
	searchText := versions.SearchText.From(window)
	normalization := versions.SearchNormalizationVersion.From(window)
	projections := []jet.Projection{position, normalization}
	anyTerm := make([]jet.BoolExpression, 0, len(terms))
	for _, term := range terms {
		flag := likeEscaped(searchText, likeContains(term))
		projections = append(projections, flag)
		anyTerm = append(anyTerm, flag)
	}
	projections = append(projections, candidateProjections()...)
	statement := jet.WITH(
		window.AS(
			jet.SELECT(
				jet.ROW_NUMBER().OVER(jet.ORDER_BY(versions.CreatedAt.DESC(), versions.ID.DESC())).AS("position"),
				versions.ID, versions.SearchText, versions.SearchNormalizationVersion,
			).FROM(
				records.INNER_JOIN(versions, versions.ID.EQ(records.CurrentVersionID).AND(versions.RecordID.EQ(records.ID))),
			).WHERE(
				records.ProjectID.EQ(jet.String(projectID)).AND(versions.ContentDeletedAt.IS_NULL()),
			).ORDER_BY(versions.CreatedAt.DESC(), versions.ID.DESC()).LIMIT(int64(scanLimit) + 1),
		),
	)(
		jet.SELECT(projections[0], projections[1:]...).FROM(
			window.
				INNER_JOIN(versions, versions.ID.EQ(windowVersion)).
				INNER_JOIN(records, records.ID.EQ(versions.RecordID)),
		).WHERE(
			position.GT(jet.Int(int64(scanLimit))).
				OR(normalization.NOT_EQ(jet.Int(searchNormalizationVersion))).
				OR(jet.OR(anyTerm...)),
		).ORDER_BY(position.ASC()),
	)
	query, args := statement.Sql()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("scan search window: %w", err)
	}
	defer rows.Close()
	var hits []compose.SearchHit
	limitReached := false
	matched := make([]bool, len(terms))
	for rows.Next() {
		var row, normalizedBy int64
		var candidate candidateRow
		destinations := []any{&row, &normalizedBy}
		for index := range matched {
			destinations = append(destinations, &matched[index])
		}
		if err := rows.Scan(append(destinations, candidate.destinations()...)...); err != nil {
			return nil, false, fmt.Errorf("scan search window: %w", ErrUndecodableMemoryRow)
		}
		if row > int64(scanLimit) {
			limitReached = true
			continue
		}
		if normalizedBy != searchNormalizationVersion {
			return nil, false, fmt.Errorf("scan search window: query uses search normalization %d: %w", searchNormalizationVersion, ErrSearchNormalizationMismatch)
		}
		hit := compose.SearchHit{MatchedTerms: countTrue(matched)}
		if hit.Candidate, err = candidate.candidate(); err != nil {
			return nil, false, err
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("scan search window: %w", err)
	}
	return hits, limitReached, nil
}

func countTrue(flags []bool) int {
	count := 0
	for _, flag := range flags {
		if flag {
			count++
		}
	}
	return count
}

// candidateProjections are a record, its current version without the author
// and redaction policy version, which no memory rule reads, and whether a human
// approved that exact version and content hash, with the stored sizes of the
// content and of the version's sources. Content above memory.MaxMaterialBytes
// is not read: it comes back NULL with its size, and the size rule excludes it.
func candidateProjections() []jet.Projection {
	records, versions := table.MemoryRecords, table.MemoryRecordVersions
	approved := exactVersionApproved()
	contentBytes := byteLength(versions.Content)
	boundedContent := jet.CASE().WHEN(contentBytes.LT_EQ(jet.Int(memory.MaxMaterialBytes))).THEN(versions.Content)
	sourceCount, sourceBytes := sourceSizes()
	return []jet.Projection{
		records.ID, records.ProjectID, records.Status, records.CurrentVersionID, records.Version, records.CreatedAt,
		versions.ID, versions.RecordID, versions.ProjectID, boundedContent, jet.COALESCE(contentBytes, jet.Int(0)), versions.ContentHash, versions.ContentDeletedAt,
		versions.Trust, versions.Visibility, versions.Origin,
		versions.RunID, versions.AttemptID, versions.BaseCommit, versions.AcceptedResultRef, versions.CreatedAt,
		approved, sourceCount, sourceBytes,
	}
}

// exactVersionApproved is whether a human approve review names the selected
// memory_record_versions row's exact version and content hash, the fact of
// memory.EffectiveTrust.
func exactVersionApproved() jet.BoolExpression {
	versions, reviews := table.MemoryRecordVersions, table.MemoryReviews
	return jet.EXISTS(
		jet.SELECT(jet.Int(1)).FROM(reviews).WHERE(
			reviews.VersionID.EQ(versions.ID).
				AND(reviews.ContentHash.EQ(versions.ContentHash)).
				AND(reviews.Actor.EQ(jet.String(string(memory.ActorHuman)))).
				AND(reviews.Decision.EQ(jet.String(string(memory.ReviewApprove)))),
		),
	)
}

// sourceSizes are the stored count and memory.SourceBytes size of the
// selected memory_record_versions row's sources, so a reader can decide
// whether to load them before reading any.
func sourceSizes() (jet.Projection, jet.Projection) {
	versions, sources := table.MemoryRecordVersions, table.MemorySources
	count := jet.SELECT(jet.COUNT(jet.STAR)).FROM(sources).WHERE(sources.VersionID.EQ(versions.ID))
	size := jet.SELECT(
		jet.COALESCE(jet.SUMi(
			jet.IntExp(jet.COALESCE(byteLength(sources.Ref), jet.Int(0))).
				ADD(jet.IntExp(jet.COALESCE(byteLength(sources.Path), jet.Int(0)))).
				ADD(jet.IntExp(jet.COALESCE(byteLength(sources.BlobHash), jet.Int(0)))),
		), jet.Int(0)),
	).FROM(sources).WHERE(sources.VersionID.EQ(versions.ID))
	return count, size
}

// byteLength is the UTF-8 size of a text value: length() of a BLOB counts
// bytes, of TEXT characters.
func byteLength(column jet.Expression) jet.IntegerExpression {
	return jet.IntExp(jet.Func("length", jet.CAST(column).AS("BLOB")))
}

// candidateRow receives candidateProjections.
type candidateRow struct {
	compose.Candidate
	recordCreated, versionCreated                                    string
	content, deletedAt, runID, attemptID, baseCommit, acceptedResult sql.NullString
}

func (r *candidateRow) destinations() []any {
	scope := &r.Version.Scope
	return []any{
		&r.Record.ID, &r.Record.ProjectID, &r.Record.Status, &r.Record.CurrentVersionID, &r.Record.Version, &r.recordCreated,
		&r.Version.ID, &r.Version.RecordID, &scope.ProjectID, &r.content, &r.ContentBytes, &r.Version.ContentHash, &r.deletedAt,
		&r.Version.Trust, &scope.Visibility, &scope.Origin,
		&r.runID, &r.attemptID, &r.baseCommit, &r.acceptedResult, &r.versionCreated,
		&r.ExactVersionApproved, &r.SourceCount, &r.SourceBytes,
	}
}

func (r *candidateRow) candidate() (compose.Candidate, error) {
	c := r.Candidate
	c.RecordFound = true
	c.Version.Content, c.ContentDeleted = r.content.String, r.deletedAt.Valid
	scope := &c.Version.Scope
	scope.RunID, scope.AttemptID, scope.BaseCommit, scope.AcceptedResultRef = r.runID.String, r.attemptID.String, r.baseCommit.String, r.acceptedResult.String
	var err error
	if c.Record.CreatedAt, err = time.Parse(time.RFC3339Nano, r.recordCreated); err != nil {
		return compose.Candidate{}, fmt.Errorf("memory record creation time: %w", ErrInvalidStoredTimestamp)
	}
	if c.VersionCreatedAt, err = time.Parse(time.RFC3339Nano, r.versionCreated); err != nil {
		return compose.Candidate{}, fmt.Errorf("memory version creation time: %w", ErrInvalidStoredTimestamp)
	}
	return c, nil
}

// readLinkedChunk reads the chunk's records of the project joined to their
// current versions.
//
// The project is matched on the version, which the (record_id, project_id)
// foreign key keeps equal to the record's. Matched on the record, Turso's
// planner prefers the project index over the id list and scans the whole
// project (measured ~8 ms instead of ~1 ms for 256 ids in 5 000 records).
func readLinkedChunk(ctx context.Context, tx *sql.Tx, projectID string, recordIDs []string) ([]compose.Candidate, error) {
	records, versions := table.MemoryRecords, table.MemoryRecordVersions
	projections := candidateProjections()
	statement := jet.SELECT(projections[0], projections[1:]...).FROM(
		records.INNER_JOIN(versions, versions.ID.EQ(records.CurrentVersionID).AND(versions.RecordID.EQ(records.ID))),
	).WHERE(versions.ProjectID.EQ(jet.String(projectID)).AND(records.ID.IN(stringList(recordIDs)...)))
	query, args := statement.Sql()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read memory records: %w", err)
	}
	defer rows.Close()
	var candidates []compose.Candidate
	for rows.Next() {
		var row candidateRow
		if err := rows.Scan(row.destinations()...); err != nil {
			return nil, fmt.Errorf("scan memory record: %w", ErrUndecodableMemoryRow)
		}
		candidate, err := row.candidate()
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory records: %w", err)
	}
	return candidates, nil
}

// attachFacts sets each candidate's version sources and record contradicts
// relations, reading them in chunks. Sources beyond memory.MaxSourcesPerItem
// or memory.MaxSourceBytes are not read; their stored count and size already
// make the size rule exclude the candidate.
func attachFacts(ctx context.Context, tx *sql.Tx, projectID string, candidates []compose.Candidate) error {
	for chunk := range slices.Chunk(candidates, memoryIDChunk) {
		versionIDs := make([]string, 0, len(chunk))
		recordIDs := make([]string, len(chunk))
		for index, candidate := range chunk {
			recordIDs[index] = candidate.Record.ID
			if candidate.SourceCount > 0 && candidate.SourceCount <= memory.MaxSourcesPerItem && candidate.SourceBytes <= memory.MaxSourceBytes {
				versionIDs = append(versionIDs, candidate.Version.ID)
			}
		}
		sources, err := readSources(ctx, tx, versionIDs)
		if err != nil {
			return err
		}
		contradicts, err := readContradicts(ctx, tx, projectID, recordIDs)
		if err != nil {
			return err
		}
		for index := range chunk {
			chunk[index].Sources = sources[chunk[index].Version.ID]
			chunk[index].Contradicts = contradicts.related[chunk[index].Record.ID]
			chunk[index].ContradictsTruncated = contradicts.truncated[chunk[index].Record.ID]
		}
	}
	return nil
}

// readSources returns each version's sources ordered by source id.
func readSources(ctx context.Context, tx database, versionIDs []string) (map[string][]memory.RecordSource, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	sources := table.MemorySources
	statement := jet.SELECT(sources.VersionID, sources.Kind, sources.Ref, sources.Path, sources.BlobHash).
		FROM(sources).WHERE(sources.VersionID.IN(stringList(versionIDs)...)).ORDER_BY(sources.VersionID, sources.ID)
	query, args := statement.Sql()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read memory sources: %w", err)
	}
	defer rows.Close()
	result := make(map[string][]memory.RecordSource)
	for rows.Next() {
		var versionID string
		var source memory.RecordSource
		var ref, path, blobHash sql.NullString
		if err := rows.Scan(&versionID, &source.Kind, &ref, &path, &blobHash); err != nil {
			return nil, fmt.Errorf("scan memory source: %w", ErrUndecodableMemoryRow)
		}
		source.Ref, source.Path, source.BlobHash = ref.String, path.String, blobHash.String
		result[versionID] = append(result[versionID], source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory sources: %w", err)
	}
	return result, nil
}

// contradictsRelations is the records' contradicts relations in either
// direction with the other record's stored status, once per pair.
type contradictsRelations struct {
	related   map[string][]compose.RelatedRecord
	truncated map[string]bool
}

// readContradicts returns, per record, at most memory.MaxContradictsPerItem
// related records ordered by record id, and which records have more. The
// engine ranks the pairs per record, so no more than one extra row per record
// is read.
func readContradicts(ctx context.Context, tx *sql.Tx, projectID string, recordIDs []string) (contradictsRelations, error) {
	relations, records := table.MemoryRelations, table.MemoryRecords
	direction := func(subject, other jet.ColumnString) jet.SelectStatement {
		return jet.SELECT(subject.AS("subject"), records.ID.AS("related"), records.Status.AS("status")).FROM(
			relations.INNER_JOIN(records, records.ID.EQ(other)),
		).WHERE(
			relations.ProjectID.EQ(jet.String(projectID)).
				AND(relations.Kind.EQ(jet.String(string(memory.RelationContradicts)))).
				AND(subject.IN(stringList(recordIDs)...)),
		)
	}
	pairs := jet.UNION(
		direction(relations.FromRecordID, relations.ToRecordID),
		direction(relations.ToRecordID, relations.FromRecordID),
	).AsTable("contradicts_pairs")
	subject := jet.StringColumn("subject").From(pairs)
	related := jet.StringColumn("related").From(pairs)
	status := jet.StringColumn("status").From(pairs)
	ranked := jet.SELECT(
		subject.AS("subject"), related.AS("related"), status.AS("status"),
		jet.ROW_NUMBER().OVER(jet.PARTITION_BY(subject).ORDER_BY(related)).AS("position"),
	).FROM(pairs).AsTable("contradicts_ranked")
	rankedSubject := jet.StringColumn("subject").From(ranked)
	rankedRelated := jet.StringColumn("related").From(ranked)
	statement := jet.SELECT(rankedSubject, rankedRelated, jet.StringColumn("status").From(ranked)).
		FROM(ranked).
		WHERE(jet.IntegerColumn("position").From(ranked).LT_EQ(jet.Int(memory.MaxContradictsPerItem+1))).
		ORDER_BY(rankedSubject, rankedRelated)
	query, args := statement.Sql()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return contradictsRelations{}, fmt.Errorf("read memory relations: %w", err)
	}
	defer rows.Close()
	result := contradictsRelations{related: make(map[string][]compose.RelatedRecord), truncated: make(map[string]bool)}
	for rows.Next() {
		var subject string
		var other compose.RelatedRecord
		if err := rows.Scan(&subject, &other.RecordID, &other.Status); err != nil {
			return contradictsRelations{}, fmt.Errorf("scan memory relation: %w", ErrUndecodableMemoryRow)
		}
		if len(result.related[subject]) == memory.MaxContradictsPerItem {
			result.truncated[subject] = true
			continue
		}
		result.related[subject] = append(result.related[subject], other)
	}
	if err := rows.Err(); err != nil {
		return contradictsRelations{}, fmt.Errorf("iterate memory relations: %w", err)
	}
	return result, nil
}

func stringList(values []string) []jet.Expression {
	expressions := make([]jet.Expression, len(values))
	for index, value := range values {
		expressions[index] = jet.String(value)
	}
	return expressions
}
