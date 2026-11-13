// Package deletion owns the DeleteRecord procedure: a human deletes the
// content of every version of one memory record. The facts, the decision and
// the writes happen in one storage transaction through a consumer-side port.
//
// Removing manifest payloads that carried the record and invalidating their
// dispatch intents (memory.DecidePayloadRemoval) is wired in task 4.3 of spec
// 012, which needs the dispatch intent of spec 003; this procedure does not
// touch payloads.
package deletion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/id"
	"github.com/MartinAndreev/conduit/internal/memory"
)

// Store runs one record deletion in one storage transaction.
type Store interface {
	// WithinRecordDeletion runs operation in one write transaction that holds
	// the database write lock from before its first read, so no other writer
	// changes the facts between the read and the writes. It commits when
	// operation returns nil and rolls back otherwise; a failed commit is
	// rolled back too.
	WithinRecordDeletion(ctx context.Context, operation func(Transaction) error) error
}

// Transaction is the read and the write of one deletion, valid only inside
// WithinRecordDeletion.
type Transaction interface {
	// ReadRecordDeletionFacts reads the record with recordID in any project
	// and all its versions. A missing record is Found false, not an error.
	ReadRecordDeletionFacts(ctx context.Context, recordID string) (RecordFacts, error)
	// DeleteRecordContent performs the deletion as one logical effect: it
	// clears content and search text and sets content_deleted_at on each
	// listed version, deletes the sources and clears the review reasons of
	// every version of the record, bumps the record's version by one from
	// ExpectedVersion and sets updated_at, and appends the receipts. It fails
	// without writing anything visible if a listed version is already deleted
	// or the record's version is not ExpectedVersion.
	DeleteRecordContent(ctx context.Context, deletion Deletion) error
}

// RecordFacts is the stored record and its versions.
type RecordFacts struct {
	Found    bool
	Record   memory.Record
	Versions []memory.VersionDeletionState
}

// Deletion is what DeleteRecordContent writes.
type Deletion struct {
	ProjectID       string
	RecordID        string
	ExpectedVersion int64
	Versions        []memory.VersionDeletionState
	// Receipts hold one memory_version receipt per listed version.
	Receipts  []memory.DeletionReceipt
	DeletedAt time.Time
}

// Request is one deletion request.
type Request struct {
	ProjectID       string
	RecordID        string
	ExpectedVersion int64
	// Actor is established by the presentation entry point, never copied from
	// a request field or an agent binding.
	Actor memory.Actor
	Now   time.Time
}

// Result reports the decision. Receipts and RecordVersion, the record's new
// optimistic version, are set only for memory.DeletionDelete.
type Result struct {
	Kind          memory.RecordDeletionKind
	Reason        string
	Receipts      []memory.DeletionReceipt
	RecordVersion int64
}

// Service exposes the deletion procedure.
type Service struct {
	Store Store
	// NewID creates receipt IDs; nil means id.New.
	NewID func(prefix string) (string, error)
}

// ErrInvalidRequest reports a request without a time. Other invalid fields
// are an invalid_facts decision.
var ErrInvalidRequest = errors.New("memory deletion request needs the current time")

// DeleteRecord reads the record's facts, decides with
// memory.DecideRecordDeletion and, only for a delete decision, deletes in the
// same transaction. Any other decision writes nothing. An error before the
// commit means nothing was written; a failed commit may leave the outcome
// unknown. Repeating the request is then safe: a committed deletion decides
// already_deleted and writes nothing more.
func (s Service) DeleteRecord(ctx context.Context, request Request) (Result, error) {
	if request.Now.IsZero() {
		return Result{}, ErrInvalidRequest
	}
	var result Result
	err := s.Store.WithinRecordDeletion(ctx, func(tx Transaction) error {
		var err error
		result, err = s.deleteIn(ctx, tx, request)
		return err
	})
	if err != nil {
		return Result{}, fmt.Errorf("delete memory record: %w", err)
	}
	return result, nil
}

// DeleteWithin is DeleteRecord inside the caller's transaction tx: it reads
// the record's facts, decides with memory.DecideRecordDeletion and, only for
// a delete decision, deletes. A procedure that must decide something else in
// the same transaction first, such as the confirmation of an agent's delete
// suggestion, calls it; the caller owns the commit. Any other decision writes
// nothing.
func (s Service) DeleteWithin(ctx context.Context, tx Transaction, request Request) (Result, error) {
	if request.Now.IsZero() {
		return Result{}, ErrInvalidRequest
	}
	return s.deleteIn(ctx, tx, request)
}

// deleteIn reads, decides and, only for a delete decision, deletes in tx.
func (s Service) deleteIn(ctx context.Context, tx Transaction, request Request) (Result, error) {
	facts, err := tx.ReadRecordDeletionFacts(ctx, request.RecordID)
	if err != nil {
		return Result{}, fmt.Errorf("read deletion facts: %w", err)
	}
	decision := memory.DecideRecordDeletion(memory.RecordDeletionFacts{
		RequestProjectID: request.ProjectID,
		RequestRecordID:  request.RecordID,
		ExpectedVersion:  request.ExpectedVersion,
		Actor:            request.Actor,
		RecordFound:      facts.Found,
		Record:           facts.Record,
		Versions:         facts.Versions,
	})
	result := Result{Kind: decision.Kind, Reason: decision.Reason}
	if decision.Kind != memory.DeletionDelete {
		return result, nil
	}
	deletion, err := s.deletion(request, decision.Versions)
	if err != nil {
		return Result{}, err
	}
	if err := tx.DeleteRecordContent(ctx, deletion); err != nil {
		return Result{}, fmt.Errorf("delete record content: %w", err)
	}
	result.Receipts, result.RecordVersion = deletion.Receipts, request.ExpectedVersion+1
	return result, nil
}

// deletion builds the writes with one receipt per version.
func (s Service) deletion(request Request, versions []memory.VersionDeletionState) (Deletion, error) {
	newID := s.NewID
	if newID == nil {
		newID = id.New
	}
	deletedAt := request.Now.UTC()
	receipts := make([]memory.DeletionReceipt, len(versions))
	for index, version := range versions {
		receiptID, err := newID("deletion")
		if err != nil {
			return Deletion{}, err
		}
		receipts[index] = memory.DeletionReceipt{
			ID:          receiptID,
			ProjectID:   request.ProjectID,
			SubjectKind: memory.DeletionSubjectMemoryVersion,
			SubjectID:   version.VersionID,
			ContentHash: version.ContentHash,
			DeletedAt:   deletedAt,
		}
	}
	return Deletion{
		ProjectID:       request.ProjectID,
		RecordID:        request.RecordID,
		ExpectedVersion: request.ExpectedVersion,
		Versions:        versions,
		Receipts:        receipts,
		DeletedAt:       deletedAt,
	}, nil
}
