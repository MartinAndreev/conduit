package records

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// Every transition below reads the record, decides with
// memory.DecideRecordTransition and writes only on an allowed decision, in
// one transaction. Any other decision writes nothing. An error before the
// commit means nothing was written; after a failed commit the outcome may be
// unknown, and a retry with the same expected version then reports
// version_conflict if the first attempt committed.

// ApproveRequest is a human's approval of a proposal's exact current version.
type ApproveRequest struct {
	ProjectID       string
	RecordID        string
	ExpectedVersion int64
	// Actor is established by the presentation entry point.
	Actor    memory.Actor
	Reviewer string
	// VersionID and ContentHash are what the approval names.
	VersionID   string
	ContentHash string
	Now         time.Time
}

// Approve moves a proposal to active and writes the approve review naming the
// version and content hash.
func (s Service) Approve(ctx context.Context, request ApproveRequest) (TransitionResult, error) {
	if err := validRequest(request.Now, request.Reviewer); err != nil {
		return TransitionResult{}, err
	}
	var result TransitionResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		stored, err := tx.ReadRecord(ctx, request.RecordID)
		if err != nil {
			return fmt.Errorf("read memory record: %w", err)
		}
		facts := transitionFacts(request.ProjectID, request.ExpectedVersion, memory.StatusActive, request.Actor, stored, request.Now)
		facts.ApprovedVersionID, facts.ApprovedContentHash, facts.Reviewer = request.VersionID, request.ContentHash, request.Reviewer
		decision := memory.DecideRecordTransition(facts)
		result = TransitionResult{Kind: decision.Kind, Reason: decision.Reason}
		if decision.Kind != memory.TransitionAllowed {
			return nil
		}
		review := Review{
			VersionID:   request.VersionID,
			ContentHash: request.ContentHash,
			Decision:    memory.ReviewApprove,
			Actor:       request.Actor,
			Reviewer:    request.Reviewer,
		}
		if err := s.writeReviewedChange(ctx, tx, statusChange(stored, request.ExpectedVersion, memory.StatusActive, request.Now), review); err != nil {
			return err
		}
		result.RecordVersion = request.ExpectedVersion + 1
		return nil
	})
	if err != nil {
		return TransitionResult{}, fmt.Errorf("approve memory record: %w", err)
	}
	return result, nil
}

// ReasonRequest is a human decision that needs a stated reason: rejecting a
// proposal or archiving an active or stale record.
type ReasonRequest struct {
	ProjectID       string
	RecordID        string
	ExpectedVersion int64
	// Actor is established by the presentation entry point.
	Actor    memory.Actor
	Reviewer string
	Reason   string
	Now      time.Time
}

// Reject moves a proposal to rejected and writes the reject review with the
// redacted reason on its current version.
func (s Service) Reject(ctx context.Context, request ReasonRequest) (TransitionResult, error) {
	return s.decideWithReason(ctx, request, memory.StatusRejected, memory.ReviewReject, "reject")
}

// Archive moves an active or stale record to archived. The reason is kept as
// the human's opinion review, with the redacted reason, on the current
// version: memory_reviews is the only place a reason is stored.
func (s Service) Archive(ctx context.Context, request ReasonRequest) (TransitionResult, error) {
	return s.decideWithReason(ctx, request, memory.StatusArchived, memory.ReviewOpinion, "archive")
}

// ArchiveWithin is Archive inside the caller's transaction tx: it redacts the
// reason, reads the record, decides and, only when allowed, writes the status
// change and the human's opinion review with the reason. A procedure that
// must decide something else in the same transaction first, such as the
// confirmation of an agent's archive suggestion, calls it; the caller owns
// the commit. A refusal writes nothing.
func (s Service) ArchiveWithin(ctx context.Context, tx Transaction, request ReasonRequest) (TransitionResult, error) {
	if err := validRequest(request.Now, request.Reviewer); err != nil {
		return TransitionResult{}, err
	}
	return s.decideWithReasonIn(ctx, tx, request, memory.StatusArchived, memory.ReviewOpinion)
}

// decideWithReason is the shared flow of Reject and Archive in their own
// transaction (decideWithReasonIn).
func (s Service) decideWithReason(ctx context.Context, request ReasonRequest, to memory.RecordStatus, decisionKind memory.ReviewDecision, action string) (TransitionResult, error) {
	if err := validRequest(request.Now, request.Reviewer); err != nil {
		return TransitionResult{}, err
	}
	var result TransitionResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		var err error
		result, err = s.decideWithReasonIn(ctx, tx, request, to, decisionKind)
		return err
	})
	if err != nil {
		return TransitionResult{}, fmt.Errorf("%s memory record: %w", action, err)
	}
	return result, nil
}

// decideWithReasonIn redacts the reason, reads, decides the transition to
// `to`, and writes the status change with a review of the current version
// carrying the reason, all in tx.
func (s Service) decideWithReasonIn(ctx context.Context, tx Transaction, request ReasonRequest, to memory.RecordStatus, decisionKind memory.ReviewDecision) (TransitionResult, error) {
	reason, findings := redactReason(request.Reason)
	stored, err := tx.ReadRecord(ctx, request.RecordID)
	if err != nil {
		return TransitionResult{}, fmt.Errorf("read memory record: %w", err)
	}
	facts := transitionFacts(request.ProjectID, request.ExpectedVersion, to, request.Actor, stored, request.Now)
	facts.Reason, facts.Reviewer = reason, request.Reviewer
	decision := memory.DecideRecordTransition(facts)
	result := TransitionResult{Kind: decision.Kind, Reason: decision.Reason, Findings: findings}
	if decision.Kind != memory.TransitionAllowed {
		return result, nil
	}
	review := Review{
		VersionID:   stored.Record.CurrentVersionID,
		ContentHash: stored.CurrentContentHash,
		Decision:    decisionKind,
		Actor:       request.Actor,
		Reviewer:    request.Reviewer,
		Reason:      reason,
	}
	if err := s.writeReviewedChange(ctx, tx, statusChange(stored, request.ExpectedVersion, to, request.Now), review); err != nil {
		return TransitionResult{}, err
	}
	result.RecordVersion = request.ExpectedVersion + 1
	return result, nil
}

// writeReviewedChange writes an allowed status change and the review that
// records the human's decision, stamped with the change's time.
func (s Service) writeReviewedChange(ctx context.Context, tx Transaction, change StatusChange, review Review) error {
	reviewID, err := s.newID("review")
	if err != nil {
		return err
	}
	review.ID, review.ReviewedAt = reviewID, change.UpdatedAt
	if err := tx.ChangeStatus(ctx, change); err != nil {
		return fmt.Errorf("change memory record status: %w", err)
	}
	if err := tx.InsertReview(ctx, review); err != nil {
		return fmt.Errorf("insert memory review: %w", err)
	}
	return nil
}
