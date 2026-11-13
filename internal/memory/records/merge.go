package records

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// MergedProposal names one proposal of a merge at the version the human saw.
type MergedProposal struct {
	RecordID        string
	ExpectedVersion int64
}

// MergeRequest is a human merging proposals into one text the human wrote.
type MergeRequest struct {
	ProjectID string
	Proposals []MergedProposal
	// Actor is established by the presentation entry point.
	Actor memory.Actor
	// Author writes the merged text, approves it and rejects the proposals.
	Author   string
	Category memory.RecordCategory
	Content  string
	Sources  []memory.RecordSource
	Now      time.Time
}

// MergedTransition is one proposal's transition decision in a merge.
// RecordVersion, the proposal's new optimistic version, is set only when the
// merge was applied.
type MergedTransition struct {
	RecordID      string
	Decision      memory.RecordTransitionDecision
	RecordVersion int64
}

// MergeResult reports every decision of a merge: the set of proposals, the
// merged record's creation, and each proposal's transition in request order.
// Transitions are decided only for an allowed set. Applied is true only when
// every decision allowed it and the merge committed; RecordID, VersionID and
// ContentHash name the new record only then. Findings count what redaction
// replaced in the merged text and its sources, by kind.
type MergeResult struct {
	Merge       memory.ProposalMergeDecision
	Creation    memory.RecordCreationDecision
	Transitions []MergedTransition
	Applied     bool
	RecordID    string
	VersionID   string
	ContentHash string
	Findings    []redaction.Finding
}

// Merge replaces proposals with one text a human wrote, in one transaction:
// the text becomes a new human record (proposed and approved by the author,
// so active, in the request's project with project visibility), and every
// merged proposal moves proposed → rejected with a reject review whose
// reason, memory.MergedReason, names the new record. memory.DecideProposalMerge
// decides the set of proposals before any read; then
// memory.DecideRecordCreation and, per proposal, memory.DecideRecordTransition
// at its expected version. Anything is written only when every decision
// allows it, so one refused proposal writes nothing. No relation is written:
// a rejected proposal is not superseded. An error before the commit means
// nothing was written.
func (s Service) Merge(ctx context.Context, request MergeRequest) (MergeResult, error) {
	if err := validRequest(request.Now, request.Author); err != nil {
		return MergeResult{}, err
	}
	merge := memory.DecideProposalMerge(memory.ProposalMergeFacts{RecordIDs: mergedRecordIDs(request.Proposals)})
	if merge.Kind != memory.ProposalMergeAllowed {
		return MergeResult{Merge: merge}, nil
	}
	creation := CreateRequest{
		Producer: memory.ProducerHuman,
		Actor:    request.Actor,
		Author:   request.Author,
		Category: request.Category,
		Content:  request.Content,
		Scope:    memory.Scope{ProjectID: request.ProjectID, Visibility: memory.VisibilityProject, Origin: memory.OriginHuman},
		Sources:  request.Sources,
		Now:      request.Now,
	}
	redacted := redactMaterial(request.Content, request.Sources)
	var result MergeResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		recordID, err := s.newID("record")
		if err != nil {
			return err
		}
		created := memory.DecideRecordCreation(creationFacts(creation, redacted, tx.SearchTextBytes(redacted.content)))
		result = MergeResult{Merge: merge, Creation: created, Findings: redacted.findings}
		proposals, allowed, err := decideMergedTransitions(ctx, tx, request, recordID, &result)
		if err != nil {
			return err
		}
		if created.Kind != memory.CreationAllowed || !allowed {
			return nil
		}
		return s.writeMerge(ctx, tx, request, recordID, creation, redacted, created, proposals, &result)
	})
	if err != nil {
		return MergeResult{}, fmt.Errorf("merge memory proposals: %w", err)
	}
	return result, nil
}

func mergedRecordIDs(proposals []MergedProposal) []string {
	ids := make([]string, len(proposals))
	for index, proposal := range proposals {
		ids[index] = proposal.RecordID
	}
	return ids
}

// decideMergedTransitions reads every merged proposal and decides its move to
// rejected with the merge's reason, recording each decision in result. It
// reports whether all were allowed and returns what was read for the writes.
func decideMergedTransitions(ctx context.Context, tx Transaction, request MergeRequest, recordID string, result *MergeResult) ([]RecordFacts, bool, error) {
	stored := make([]RecordFacts, len(request.Proposals))
	allowed := true
	for index, proposal := range request.Proposals {
		facts, err := tx.ReadRecord(ctx, proposal.RecordID)
		if err != nil {
			return nil, false, fmt.Errorf("read memory record: %w", err)
		}
		stored[index] = facts
		transition := transitionFacts(request.ProjectID, proposal.ExpectedVersion, memory.StatusRejected, request.Actor, facts, request.Now)
		transition.Reason = memory.MergedReason(recordID)
		transition.Reviewer = request.Author
		decision := memory.DecideRecordTransition(transition)
		result.Transitions = append(result.Transitions, MergedTransition{RecordID: proposal.RecordID, Decision: decision})
		allowed = allowed && decision.Kind == memory.TransitionAllowed
	}
	return stored, allowed, nil
}

// writeMerge writes an allowed merge: the new record with its creator's
// approval, then each proposal's status change with its reject review.
func (s Service) writeMerge(ctx context.Context, tx Transaction, request MergeRequest, recordID string, creation CreateRequest, redacted material, decision memory.RecordCreationDecision, proposals []RecordFacts, result *MergeResult) error {
	created, err := s.writeNewRecord(ctx, tx, recordID, creation, redacted, decision)
	if err != nil {
		return err
	}
	for index, stored := range proposals {
		expected := request.Proposals[index].ExpectedVersion
		review := Review{
			VersionID:   stored.Record.CurrentVersionID,
			ContentHash: stored.CurrentContentHash,
			Decision:    memory.ReviewReject,
			Actor:       request.Actor,
			Reviewer:    request.Author,
			Reason:      memory.MergedReason(recordID),
		}
		if err := s.writeReviewedChange(ctx, tx, statusChange(stored, expected, memory.StatusRejected, request.Now), review); err != nil {
			return err
		}
		result.Transitions[index].RecordVersion = expected + 1
	}
	result.Applied = true
	result.RecordID, result.VersionID, result.ContentHash = created.Record.ID, created.Version.ID, created.Version.ContentHash
	return nil
}
