package records

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// OpinionRequest is an agent's or the system's opinion about one exact
// version. An agent's audit opinion also suggests an action (task 5.5).
type OpinionRequest struct {
	ProjectID   string
	RecordID    string
	VersionID   string
	ContentHash string
	Actor       memory.Actor
	Reviewer    string
	// Reason is optional without a suggestion and required with one.
	Reason string
	// Suggestion is empty for a plain opinion.
	Suggestion memory.Suggestion
	Now        time.Time
}

// OpinionResult reports the decision. ReviewID is set only for
// memory.OpinionAdd. Findings count what redaction replaced in the reason.
type OpinionResult struct {
	Kind     memory.RecordOpinionKind
	Reason   string
	ReviewID string
	Findings []redaction.Finding
}

// AddOpinion redacts the reason, reads the record and the named version,
// decides with memory.DecideRecordOpinion and, only when allowed, writes the
// opinion review with its suggestion, in one transaction. An opinion names
// its exact version and content hash instead of an expected record version,
// and does not change the record: it never changes trust, lifecycle or
// eligibility. A suggestion is only a stored proposal; a human confirms it
// separately.
func (s Service) AddOpinion(ctx context.Context, request OpinionRequest) (OpinionResult, error) {
	if request.Now.IsZero() {
		return OpinionResult{}, ErrInvalidRequest
	}
	reason, findings := redactReason(request.Reason)
	var result OpinionResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		stored, err := tx.ReadRecord(ctx, request.RecordID)
		if err != nil {
			return fmt.Errorf("read memory record: %w", err)
		}
		version, err := tx.ReadVersion(ctx, request.RecordID, request.VersionID)
		if err != nil {
			return fmt.Errorf("read memory version: %w", err)
		}
		decision := memory.DecideRecordOpinion(memory.RecordOpinionFacts{
			RequestProjectID:  request.ProjectID,
			RequestRecordID:   request.RecordID,
			VersionID:         request.VersionID,
			ContentHash:       request.ContentHash,
			Actor:             request.Actor,
			Reviewer:          request.Reviewer,
			Reason:            reason,
			RecordFound:       stored.Found,
			Record:            stored.Record,
			VersionFound:      version.Found,
			StoredContentHash: version.ContentHash,
			ContentDeleted:    version.ContentDeleted,
			Suggestion:        request.Suggestion,
		})
		result = OpinionResult{Kind: decision.Kind, Reason: decision.Reason, Findings: findings}
		if decision.Kind != memory.OpinionAdd {
			return nil
		}
		reviewID, err := s.newID("review")
		if err != nil {
			return err
		}
		review := Review{
			ID:          reviewID,
			VersionID:   request.VersionID,
			ContentHash: request.ContentHash,
			Decision:    memory.ReviewOpinion,
			Actor:       request.Actor,
			Reviewer:    request.Reviewer,
			Reason:      reason,
			Suggestion:  request.Suggestion,
			ReviewedAt:  request.Now.UTC(),
		}
		if err := tx.InsertReview(ctx, review); err != nil {
			return fmt.Errorf("insert memory opinion: %w", err)
		}
		result.ReviewID = reviewID
		return nil
	})
	if err != nil {
		return OpinionResult{}, fmt.Errorf("add memory opinion: %w", err)
	}
	return result, nil
}
