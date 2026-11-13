package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// Service exposes the audit procedures.
type Service struct {
	// Records adds the agent's opinions and archives a confirmed record.
	Records records.Service
	// Deletion deletes a confirmed record's content.
	Deletion deletion.Service
	// Material reads the records an audit agent reviews.
	Material MaterialReader
	// Suggestions reads the agent suggestions of a project.
	Suggestions SuggestionReader
	// Confirmations runs one confirmation's transaction.
	Confirmations ConfirmationStore
}

// ErrInvalidRequest reports a request without the current time or, for a
// confirmation, without a reviewer.
var ErrInvalidRequest = errors.New("audit request needs the current time and, for a confirmation, a reviewer")

// policy redacts each decoded reason of the report's suggestions (spec 012
// "Redaction"); the report as a whole is not redacted here, because nothing
// outside those fields is stored by this path.
var policy redaction.Redactor = redaction.Policy{}

// RecordRequest is one audit agent's report about a project. Reviewer is the
// agent.
type RecordRequest struct {
	Report    string
	ProjectID string
	Reviewer  string
	Now       time.Time
}

// Recorded is one kept suggestion's opinion. Result.Findings count what the
// opinion's own redaction replaced; Extract already redacted the reason, so
// they are normally empty.
type Recorded struct {
	// Entry is the suggestion's 1-based position in the block.
	Entry  int
	Result records.OpinionResult
}

// RecordResult is the report's extraction and the opinion of each kept
// suggestion, in report order.
type RecordResult struct {
	Extraction Extraction
	Recorded   []Recorded
}

// RecordSuggestions records an audit agent's suggestions. It reads the report
// and redacts each decoded reason (Extract), then adds each kept suggestion
// as the agent's opinion with records.Service.AddOpinion (actor agent, the
// request's reviewer), each in its own transaction. A report over
// memory.MaxReportBytes records nothing; the result carries its extraction.
//
// A refusal of the opinion (for example version_not_current, hash_mismatch,
// status_not_applicable or reason_too_long) is that suggestion's Recorded
// result, not a drop: Dropped counts only DecideSuggestionKeep's decisions.
// Cancellation before extraction returns only the error. Recording stops at
// the first error or cancellation: the result then lists the suggestions
// already recorded, which stay committed, and the error names the entry that
// failed; the outcome of a failed commit may be unknown.
func (s Service) RecordSuggestions(ctx context.Context, request RecordRequest) (RecordResult, error) {
	if request.Now.IsZero() {
		return RecordResult{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return RecordResult{}, fmt.Errorf("record audit suggestions: %w", err)
	}
	result := RecordResult{Extraction: Extract(request.Report, policy)}
	for _, suggestion := range result.Extraction.Suggestions {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("record audit suggestion entry %d: %w", suggestion.Entry, err)
		}
		opinion, err := s.Records.AddOpinion(ctx, records.OpinionRequest{
			ProjectID:   request.ProjectID,
			RecordID:    suggestion.RecordID,
			VersionID:   suggestion.VersionID,
			ContentHash: suggestion.ContentHash,
			Actor:       memory.ActorAgent,
			Reviewer:    request.Reviewer,
			Reason:      suggestion.Reason,
			Suggestion:  suggestion.Action,
			Now:         request.Now,
		})
		if err != nil {
			return result, fmt.Errorf("record audit suggestion entry %d: %w", suggestion.Entry, err)
		}
		result.Recorded = append(result.Recorded, Recorded{Entry: suggestion.Entry, Result: opinion})
	}
	return result, nil
}
