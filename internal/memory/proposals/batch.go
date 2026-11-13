package proposals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
)

// DecisionKind is one review decision in a batch.
type DecisionKind string

const (
	DecisionApprove DecisionKind = "approve"
	DecisionReject  DecisionKind = "reject"
	DecisionMerge   DecisionKind = "merge"
)

// Decision is one item of a review batch. Approve and reject name one
// proposal at the version the human saw; merge names its proposals in
// Merge.
type Decision struct {
	Kind            DecisionKind
	RecordID        string
	ExpectedVersion int64
	// VersionID and ContentHash are what an approval names.
	VersionID   string
	ContentHash string
	// Reason is a rejection's reason.
	Reason string
	Merge  MergeDecision
}

// MergeDecision is the merged proposals and the text the human wrote.
type MergeDecision struct {
	Proposals []records.MergedProposal
	Category  memory.RecordCategory
	Content   string
	Sources   []memory.RecordSource
}

// BatchRequest is one human's review decisions in one project.
type BatchRequest struct {
	ProjectID string
	// Actor is established by the presentation entry point.
	Actor     memory.Actor
	Reviewer  string
	Decisions []Decision
	Now       time.Time
}

// DecisionResult is one decision's outcome. Transition is set for approve
// and reject, Merge for merge. Applied is true when the decision's
// transaction wrote it.
type DecisionResult struct {
	Kind       DecisionKind
	Applied    bool
	Transition records.TransitionResult
	Merge      records.MergeResult
}

// BatchResult lists the decided items in request order.
type BatchResult struct {
	Results []DecisionResult
}

// ErrInvalidBatch reports a batch without the current time or a reviewer, or
// with an unknown decision kind. Nothing is applied.
var ErrInvalidBatch = errors.New("review batch needs the current time, a reviewer and known decision kinds")

// decider applies one decision of a batch in its own transaction.
type decider func(Service, context.Context, BatchRequest, Decision) (DecisionResult, error)

// deciders is the one list of decision kinds.
var deciders = map[DecisionKind]decider{
	DecisionApprove: Service.approve,
	DecisionReject:  Service.reject,
	DecisionMerge:   Service.merge,
}

// ReviewBatch applies a human's review decisions in request order, each in
// its own transaction through records.Service (Approve, Reject, Merge), so
// partial success is expected and visible per item. A refusal is that
// item's result and the batch continues. A technical error or cancellation
// stops it: the result lists the items decided before it, which stay
// committed, the error names the failing item, and later items are not
// attempted. An item whose commit failed may have an unknown outcome.
func (s Service) ReviewBatch(ctx context.Context, request BatchRequest) (BatchResult, error) {
	if request.Now.IsZero() || request.Reviewer == "" {
		return BatchResult{}, ErrInvalidBatch
	}
	for _, decision := range request.Decisions {
		if _, known := deciders[decision.Kind]; !known {
			return BatchResult{}, ErrInvalidBatch
		}
	}
	var result BatchResult
	for index, decision := range request.Decisions {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("review decision %d: %w", index, err)
		}
		decided, err := deciders[decision.Kind](s, ctx, request, decision)
		if err != nil {
			return result, fmt.Errorf("review decision %d: %w", index, err)
		}
		result.Results = append(result.Results, decided)
	}
	return result, nil
}

func (s Service) approve(ctx context.Context, request BatchRequest, decision Decision) (DecisionResult, error) {
	transition, err := s.Records.Approve(ctx, records.ApproveRequest{
		ProjectID:       request.ProjectID,
		RecordID:        decision.RecordID,
		ExpectedVersion: decision.ExpectedVersion,
		Actor:           request.Actor,
		Reviewer:        request.Reviewer,
		VersionID:       decision.VersionID,
		ContentHash:     decision.ContentHash,
		Now:             request.Now,
	})
	return DecisionResult{Kind: DecisionApprove, Applied: transition.Kind == memory.TransitionAllowed, Transition: transition}, err
}

func (s Service) reject(ctx context.Context, request BatchRequest, decision Decision) (DecisionResult, error) {
	transition, err := s.Records.Reject(ctx, records.ReasonRequest{
		ProjectID:       request.ProjectID,
		RecordID:        decision.RecordID,
		ExpectedVersion: decision.ExpectedVersion,
		Actor:           request.Actor,
		Reviewer:        request.Reviewer,
		Reason:          decision.Reason,
		Now:             request.Now,
	})
	return DecisionResult{Kind: DecisionReject, Applied: transition.Kind == memory.TransitionAllowed, Transition: transition}, err
}

func (s Service) merge(ctx context.Context, request BatchRequest, decision Decision) (DecisionResult, error) {
	merged, err := s.Records.Merge(ctx, records.MergeRequest{
		ProjectID: request.ProjectID,
		Proposals: decision.Merge.Proposals,
		Actor:     request.Actor,
		Author:    request.Reviewer,
		Category:  decision.Merge.Category,
		Content:   decision.Merge.Content,
		Sources:   decision.Merge.Sources,
		Now:       request.Now,
	})
	return DecisionResult{Kind: DecisionMerge, Applied: merged.Applied, Merge: merged}, err
}
