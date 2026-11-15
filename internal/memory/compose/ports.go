package compose

import (
	"context"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// MemoryReader reads memory facts for one composition. It only reads; the
// rules decide. Both methods are plain SQL reads over memory_records,
// memory_record_versions, memory_sources, memory_reviews and memory_relations.
type MemoryReader interface {
	// ReadLinkedRecords returns, for each requested record ID that exists in
	// the project, the record with its current version. IDs that do not exist
	// are absent from the result; that is not an error.
	ReadLinkedRecords(ctx context.Context, projectID string, recordIDs []string) ([]Candidate, error)
	// SearchRecords scans the project's newest ScanLimit current versions
	// whose content is not deleted, matching or not, and returns those whose
	// normalized search text contains at least one normalized query term.
	// LimitReached reports that the project has more such versions. The
	// reader owns query normalization and splitting, matching how it wrote
	// search_text, and reports how many terms the query had.
	SearchRecords(ctx context.Context, query SearchQuery) (SearchResult, error)
}

// Candidate is one record version with the facts the memory rules need.
type Candidate struct {
	// RecordFound is false when a search hit names a record with no local
	// row; Record.ID still carries the hit's record ID.
	RecordFound bool
	// Record is the memory_records row; Record.Version is its optimistic
	// version.
	Record memory.Record
	// Version is the hit's version for search, or the current version for a
	// linked read. Version.ID is empty when no local version exists. Content
	// is the stored text and is empty when ContentDeleted. Author and
	// RedactionPolicyVersion are not read: no memory rule uses them, and
	// redaction applies the current policy.
	Version memory.RecordVersion
	// VersionCreatedAt is memory_record_versions.created_at, the recency.
	VersionCreatedAt time.Time
	// ContentDeleted is true when content_deleted_at is set.
	ContentDeleted bool
	// ContentBytes is the stored content's size in UTF-8 bytes, 0 when
	// deleted. The reader loads Content only when ContentBytes is within
	// memory.MaxMaterialBytes; above it Content is empty and the size rule
	// excludes the candidate.
	ContentBytes int64
	// Sources are the version's memory_sources rows. SourceCount and
	// SourceBytes (memory.SourceBytes) describe all stored rows; the reader
	// loads Sources only when both are within memory.MaxSourcesPerItem and
	// memory.MaxSourceBytes.
	Sources     []memory.RecordSource
	SourceCount int64
	SourceBytes int64
	// ExactVersionApproved is true when a human approval review names this
	// version and its content hash.
	ExactVersionApproved bool
	// Contradicts lists the records joined to this one by a contradicts
	// relation in either direction, with their stored status, at most
	// memory.MaxContradictsPerItem of them. ContradictsTruncated is true when
	// the record has more; the conflict rule then labels the item.
	Contradicts          []RelatedRecord
	ContradictsTruncated bool
}

// RelatedRecord is the other side of a relation.
type RelatedRecord struct {
	RecordID string
	Status   memory.RecordStatus
}

// SearchQuery is the raw query text; ScanLimit bounds the rows scanned.
type SearchQuery struct {
	ProjectID string
	Text      string
	ScanLimit int
}

// SearchHit is a candidate with the number of distinct query terms its
// search text matched.
type SearchHit struct {
	Candidate
	MatchedTerms int
}

// SearchResult reports the hits in the reader's order, the query's term
// count, and whether the scan stopped at its limit.
type SearchResult struct {
	Hits         []SearchHit
	QueryTerms   int
	LimitReached bool
}

// SnapshotRef identifies the target workspace snapshot; its meaning belongs
// to the snapshot reader. The git workspace reader (internal/memoryinfra)
// takes the absolute path of the workspace directory, so the snapshot is the
// working tree with its uncommitted changes.
type SnapshotRef string

// SnapshotReader observes files of the target workspace snapshot.
type SnapshotReader interface {
	// ObserveFiles observes every path in the snapshot with one read of it
	// and returns one observation per path, in the order of paths. A present
	// file's hash is in the same format as a file source's blob hash. A
	// missing or unreadable file is an observation, not an error; an error
	// means the snapshot itself could not be read, and then no observation is
	// returned. An error must not contain a path or file contents:
	// composition returns it to its caller before any redaction.
	ObserveFiles(ctx context.Context, snapshot SnapshotRef, paths []string) ([]FileObservation, error)
}

// FileObservation is one file in the snapshot. Hash is set only for present.
type FileObservation struct {
	Observation memory.SourceObservation
	Hash        string
}
