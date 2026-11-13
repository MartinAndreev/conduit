package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

// ConfirmationStore runs one confirmation in one storage transaction.
type ConfirmationStore interface {
	// WithinSuggestionConfirmation runs operation in one write transaction
	// that holds the database write lock from before its first read, so no
	// other writer changes the facts between the reads and the writes. It
	// commits when operation returns nil and rolls back otherwise; a failed
	// commit is rolled back too.
	WithinSuggestionConfirmation(ctx context.Context, operation func(ConfirmationTransaction) error) error
}

// ConfirmationTransaction is the reads and writes of one confirmation, valid
// only inside WithinSuggestionConfirmation: the suggestion's facts and the
// record write and deletion transactions of the confirmed action.
type ConfirmationTransaction interface {
	records.Transaction
	deletion.Transaction
	// ReadSuggestion reads the review with reviewID, in any project, and
	// the current state of the record of the version it names. A missing
	// review is Standing.ReviewFound false, not an error.
	ReadSuggestion(ctx context.Context, reviewID string) (StoredSuggestion, error)
}

// ConfirmRequest is a human's confirmation of one suggestion. There is no
// batch: each confirmation names one suggestion (Martin, 2026-11-13).
type ConfirmRequest struct {
	ProjectID string
	ReviewID  string
	// ExpectedVersion is the record's version the human saw.
	ExpectedVersion int64
	// Actor is established by the presentation entry point, never copied
	// from a request field or an agent binding.
	Actor    memory.Actor
	Reviewer string
	// Reason is the human's own reason, required to archive. The agent's
	// reason is not copied.
	Reason string
	Now    time.Time
}

// ConfirmResult reports the confirmation. Kind is
// memory.DecideSuggestionConfirmation's; for memory.SuggestionApplies,
// Action names the action, and Transition (archive) or Deletion (delete) is
// that action's own decision. Applied is true when the action was written.
type ConfirmResult struct {
	Kind       memory.SuggestionKind
	Reason     string
	Action     memory.Suggestion
	Applied    bool
	Transition records.TransitionResult
	Deletion   deletion.Result
}

// confirmer performs one confirmed action inside the confirmation's
// transaction.
type confirmer func(Service, context.Context, ConfirmationTransaction, ConfirmRequest, string) (ConfirmResult, error)

// confirmers is the one map from a suggestion to its action; a test keeps it
// equal to memory.Suggestions.
var confirmers = map[memory.Suggestion]confirmer{
	memory.SuggestArchive: Service.confirmArchive,
	memory.SuggestDelete:  Service.confirmDelete,
}

// ConfirmSuggestion confirms one agent suggestion, in one transaction: it
// reads the suggestion and its record, decides with
// memory.DecideSuggestionConfirmation (an agent or system cannot confirm; a
// stale, deleted or status-moved suggestion no longer applies) and, only when
// it applies, performs the action through the existing procedure in the same
// transaction: records.Service.ArchiveWithin (memory.DecideRecordTransition
// to archived, with the human's own reason) or deletion.Service.DeleteWithin
// (memory.DecideRecordDeletion). The request's expected version guards the
// action against a concurrent change. Any refusal writes nothing. An error
// before the commit means nothing was written; after a failed commit the
// outcome may be unknown, and a retry then reports the action's own
// version_conflict, already_deleted or status_moved.
func (s Service) ConfirmSuggestion(ctx context.Context, request ConfirmRequest) (ConfirmResult, error) {
	if request.Now.IsZero() || request.Reviewer == "" {
		return ConfirmResult{}, ErrInvalidRequest
	}
	var result ConfirmResult
	err := s.Confirmations.WithinSuggestionConfirmation(ctx, func(tx ConfirmationTransaction) error {
		stored, err := tx.ReadSuggestion(ctx, request.ReviewID)
		if err != nil {
			return fmt.Errorf("read audit suggestion: %w", err)
		}
		stored.Standing.RequestProjectID = request.ProjectID
		decision := memory.DecideSuggestionConfirmation(memory.SuggestionConfirmationFacts{
			ReviewID: request.ReviewID,
			Actor:    request.Actor,
			Standing: stored.Standing,
		})
		result = ConfirmResult{Kind: decision.Kind, Reason: decision.Reason, Action: decision.Action}
		if decision.Kind != memory.SuggestionApplies {
			return nil
		}
		confirmed, err := confirmers[decision.Action](s, ctx, tx, request, stored.Standing.Record.ID)
		if err != nil {
			return err
		}
		result.Applied, result.Transition, result.Deletion = confirmed.Applied, confirmed.Transition, confirmed.Deletion
		return nil
	})
	if err != nil {
		return ConfirmResult{}, fmt.Errorf("confirm audit suggestion: %w", err)
	}
	return result, nil
}

func (s Service) confirmArchive(ctx context.Context, tx ConfirmationTransaction, request ConfirmRequest, recordID string) (ConfirmResult, error) {
	transition, err := s.Records.ArchiveWithin(ctx, tx, records.ReasonRequest{
		ProjectID:       request.ProjectID,
		RecordID:        recordID,
		ExpectedVersion: request.ExpectedVersion,
		Actor:           request.Actor,
		Reviewer:        request.Reviewer,
		Reason:          request.Reason,
		Now:             request.Now,
	})
	if err != nil {
		return ConfirmResult{}, fmt.Errorf("archive memory record: %w", err)
	}
	return ConfirmResult{Applied: transition.Kind == memory.TransitionAllowed, Transition: transition}, nil
}

func (s Service) confirmDelete(ctx context.Context, tx ConfirmationTransaction, request ConfirmRequest, recordID string) (ConfirmResult, error) {
	deleted, err := s.Deletion.DeleteWithin(ctx, tx, deletion.Request{
		ProjectID:       request.ProjectID,
		RecordID:        recordID,
		ExpectedVersion: request.ExpectedVersion,
		Actor:           request.Actor,
		Now:             request.Now,
	})
	if err != nil {
		return ConfirmResult{}, fmt.Errorf("delete memory record: %w", err)
	}
	return ConfirmResult{Applied: deleted.Kind == memory.DeletionDelete, Deletion: deleted}, nil
}
