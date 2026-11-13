package proposals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// Service exposes the proposal procedures.
type Service struct {
	// Records creates proposals and applies review decisions.
	Records records.Service
	// Normalizer defines duplicates within one report.
	Normalizer SearchNormalizer
	// Queue reads the review queue.
	Queue QueueReader
}

// ErrInvalidRequest reports a request without the current time.
var ErrInvalidRequest = errors.New("proposal request needs the current time")

// policy redacts each decoded content, ref and path of the report's
// proposals (spec 012 "Redaction"); the report as a whole is not redacted
// here, because nothing outside those fields is stored by this path.
var policy redaction.Redactor = redaction.Policy{}

// RecordRequest is one agent report of an attempt. The scope fields name the
// attempt that produced it; Author is the agent.
type RecordRequest struct {
	Report     string
	ProjectID  string
	RunID      string
	AttemptID  string
	BaseCommit string
	Author     string
	Now        time.Time
}

// Recorded is one kept proposal's creation. Result.Findings count what the
// creation's own redaction replaced; Extract already redacted the decoded
// fields, so they are normally empty.
type Recorded struct {
	// Entry is the proposal's 1-based position in the block.
	Entry  int
	Result records.CreateResult
}

// RecordResult is the report's extraction and the creation of each kept
// proposal, in report order.
type RecordResult struct {
	Extraction Extraction
	Recorded   []Recorded
}

// RecordProposals records an agent report's proposals. It reads the report
// and redacts each decoded field (Extract), then creates each kept proposal
// with records.Service.CreateRecord as memory.ProducerAgentProposal (so
// agent_claim, proposed) with attempt visibility and origin in the request's
// attempt, each in its own transaction. A report over
// memory.MaxReportBytes records nothing; the result carries its extraction
// (BlockTooLarge).
//
// A creation refusal (for example content_too_large, or invalid_scope for an
// incomplete attempt scope) is that proposal's Recorded result, not a drop:
// Dropped counts only DecideProposalKeep's decisions, and a refused proposal
// keeps its place within the limit. Cancellation before extraction returns
// only the error. Creation stops at the first error or
// cancellation: the result then lists the proposals already recorded, which
// stay committed, and the error names the entry that failed; the outcome of a
// failed commit may be unknown.
func (s Service) RecordProposals(ctx context.Context, request RecordRequest) (RecordResult, error) {
	if request.Now.IsZero() {
		return RecordResult{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return RecordResult{}, fmt.Errorf("record proposals: %w", err)
	}
	result := RecordResult{Extraction: Extract(request.Report, policy, s.Normalizer)}
	scope := memory.Scope{
		ProjectID:  request.ProjectID,
		Visibility: memory.VisibilityAttempt,
		Origin:     memory.OriginAttempt,
		RunID:      request.RunID,
		AttemptID:  request.AttemptID,
		BaseCommit: request.BaseCommit,
	}
	for _, proposal := range result.Extraction.Proposals {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("record proposal entry %d: %w", proposal.Entry, err)
		}
		created, err := s.Records.CreateRecord(ctx, records.CreateRequest{
			Producer: memory.ProducerAgentProposal,
			Actor:    memory.ActorSystem,
			Author:   request.Author,
			Category: proposal.Category,
			Content:  proposal.Content,
			Scope:    scope,
			Sources:  proposal.Sources,
			Now:      request.Now,
		})
		if err != nil {
			return result, fmt.Errorf("record proposal entry %d: %w", proposal.Entry, err)
		}
		result.Recorded = append(result.Recorded, Recorded{Entry: proposal.Entry, Result: created})
	}
	return result, nil
}
