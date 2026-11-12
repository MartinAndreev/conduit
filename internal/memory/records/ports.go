// Package records owns the write procedures of memory records: creating a
// record and moving it through its lifecycle, and adding opinions. Each
// procedure reads its facts, decides with a rule of internal/memory and writes
// only on an allowed decision, all in one storage transaction through a
// consumer-side port.
//
// The atomic manifest, payload and dispatch intent port waits for the
// dispatch intent of spec 003. Proposal parsing and deduplication, merging,
// promotion of attempt observations on acceptance, and contradicts relations
// are not written here.
package records

import (
	"context"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// Store runs one record write in one storage transaction.
type Store interface {
	// WithinRecordWrite runs operation in one write transaction that holds
	// the database write lock from before its first read, so no other writer
	// changes the facts between the read and the writes. It commits when
	// operation returns nil and rolls back otherwise; a failed commit is
	// rolled back too.
	WithinRecordWrite(ctx context.Context, operation func(Transaction) error) error
}

// Transaction is the reads and writes of one record write, valid only inside
// WithinRecordWrite. Every guarded write fails without writing anything
// visible when the rows differ from what it names; the caller's transaction
// then rolls back.
type Transaction interface {
	// ReadRecord reads the record with recordID in any project, its current
	// version's hash, deletion state and sources, and whether a human already
	// approved or rejected any of its versions. A missing record is Found
	// false, not an error.
	ReadRecord(ctx context.Context, recordID string) (RecordFacts, error)
	// ReadVersion reads version versionID of record recordID. A version that
	// does not exist or belongs to another record is Found false.
	ReadVersion(ctx context.Context, recordID, versionID string) (VersionFacts, error)
	// ReadExpiredProposals returns at most limit proposed records, in any
	// project, created at or before cutoff and strictly after the position
	// after in (created_at, id) order, in that order. A zero after starts at
	// the oldest.
	ReadExpiredProposals(ctx context.Context, cutoff time.Time, after ProposalPosition, limit int) ([]ProposalPosition, error)
	// SearchTextBytes is the UTF-8 size of content's normalized search text
	// as storage would write it. It performs no I/O.
	SearchTextBytes(content string) int64

	// InsertRecord writes a new record, its first version with the search
	// text storage derives from the content, and that version's sources.
	InsertRecord(ctx context.Context, record NewRecord) error
	// ChangeStatus moves the record from From to To, optionally to a new
	// current version, bumps its version by one from ExpectedVersion and sets
	// updated_at. It fails when the record is not in the project, not at
	// ExpectedVersion or not in From.
	ChangeStatus(ctx context.Context, change StatusChange) error
	// CopyVersion writes a new version of the record whose content, search
	// text, author, trust, redaction policy version and scope are those of
	// FromVersionID, with Sources as its sources. It fails when FromVersionID
	// is not the record's or its content was deleted.
	CopyVersion(ctx context.Context, version VersionCopy) error
	// InsertReview writes a review of an exact version and content hash. A
	// review with a reason fails when the version's content was deleted.
	InsertReview(ctx context.Context, review Review) error
	// InsertRelation writes a relation between two records of one project.
	InsertRelation(ctx context.Context, relation Relation) error
}

// RecordFacts is the stored record and its current version.
type RecordFacts struct {
	Found                 bool
	Record                memory.Record
	CurrentContentHash    string
	CurrentContentDeleted bool
	// Sources are the current version's declared sources.
	Sources []memory.RecordSource
	// HumanDecisionRecorded is true when a human approve or reject review
	// names any version of the record.
	HumanDecisionRecorded bool
}

// ProposalPosition is a proposal's place in the sweep order.
type ProposalPosition struct {
	CreatedAt time.Time
	RecordID  string
}

// VersionFacts is one stored version.
type VersionFacts struct {
	Found          bool
	ContentHash    string
	ContentDeleted bool
}

// NewRecord is what InsertRecord writes. Record.Version is 1 and
// Record.CurrentVersionID is Version.ID.
type NewRecord struct {
	Record    memory.Record
	Version   memory.RecordVersion
	Sources   []memory.RecordSource
	CreatedAt time.Time
}

// StatusChange is what ChangeStatus writes. An empty CurrentVersionID keeps
// the current version.
type StatusChange struct {
	ProjectID        string
	RecordID         string
	ExpectedVersion  int64
	From             memory.RecordStatus
	To               memory.RecordStatus
	CurrentVersionID string
	UpdatedAt        time.Time
}

// VersionCopy is what CopyVersion writes.
type VersionCopy struct {
	ProjectID     string
	RecordID      string
	FromVersionID string
	VersionID     string
	Sources       []memory.RecordSource
	CreatedAt     time.Time
}

// Review is one memory_reviews row. An empty Reason is stored as absent.
type Review struct {
	ID          string
	VersionID   string
	ContentHash string
	Decision    memory.ReviewDecision
	Actor       memory.Actor
	Reviewer    string
	Reason      string
	ReviewedAt  time.Time
}

// Relation is one memory_relations row.
type Relation struct {
	ProjectID    string
	FromRecordID string
	ToRecordID   string
	Kind         memory.RelationKind
	CreatedAt    time.Time
}
