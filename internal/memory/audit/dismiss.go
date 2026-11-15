package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/id"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// DismissRequest is a human's dismissal of one suggestion (Martin, 7а,
// 2026-11-15). There is no batch: each dismissal names one suggestion.
type DismissRequest struct {
	ProjectID string
	ReviewID  string
	// ExpectedVersion is the record's version the human saw.
	ExpectedVersion int64
	// Actor is established by the presentation entry point, never copied
	// from a request field or an agent binding.
	Actor    memory.Actor
	Reviewer string
	// Reason is the human's optional reason; it is redacted before the
	// decision and stored only with the dismissal.
	Reason string
	Now    time.Time
}

// DismissResult reports the dismissal. Kind is
// memory.DecideSuggestionDismissal's; for memory.SuggestionDismiss, Action
// names the dismissed action and DismissalID the human's review. Findings
// count what redaction replaced in the reason.
type DismissResult struct {
	Kind        memory.SuggestionDismissalKind
	Reason      string
	Action      memory.Suggestion
	DismissalID string
	Findings    []redaction.Finding
}

// DismissSuggestion dismisses one agent suggestion. An invalid request or a
// non-human actor is refused from the request facts before the transaction,
// without any read. Otherwise, in one transaction, it reads the suggestion,
// its record and whether the action was already dismissed for the version
// and content hash, decides with memory.DecideSuggestionDismissal (a stale,
// deleted, status-moved or already dismissed suggestion, a record at another
// version than the expected one, or an over-long reason is refused) and,
// only on dismiss, writes the human's dismissal review: decision dismiss, the
// suggestion's version and content hash, the dismissed action and the
// redacted reason. It changes neither the record nor the agent's review.
// Any refusal writes nothing. An error before the commit means nothing was
// written; after a failed commit the outcome may be unknown, and a retry
// then reports already_dismissed.
func (s Service) DismissSuggestion(ctx context.Context, request DismissRequest) (DismissResult, error) {
	if request.Now.IsZero() || request.Reviewer == "" {
		return DismissResult{}, ErrInvalidRequest
	}
	reason, findings := redactReason(request.Reason)
	// The request facts alone decide invalid_facts and actor_not_allowed;
	// refusing them before the transaction reads nothing, so an agent or
	// system caller learns nothing about the review, not even a read error.
	if early := memory.DecideSuggestionDismissal(memory.SuggestionDismissalFacts{
		ReviewID:        request.ReviewID,
		Actor:           request.Actor,
		Reviewer:        request.Reviewer,
		ExpectedVersion: request.ExpectedVersion,
		Reason:          reason,
		Standing:        memory.SuggestionStandingFacts{RequestProjectID: request.ProjectID},
	}); early.Kind == memory.SuggestionDismissalInvalidFacts || early.Kind == memory.SuggestionDismissalActorNotAllowed {
		return DismissResult{Kind: early.Kind, Reason: early.Reason, Findings: findings}, nil
	}
	var result DismissResult
	err := s.Confirmations.WithinSuggestionConfirmation(ctx, func(tx ConfirmationTransaction) error {
		stored, err := tx.ReadSuggestion(ctx, request.ReviewID)
		if err != nil {
			return fmt.Errorf("read audit suggestion: %w", err)
		}
		stored.Standing.RequestProjectID = request.ProjectID
		decision := memory.DecideSuggestionDismissal(memory.SuggestionDismissalFacts{
			ReviewID:        request.ReviewID,
			Actor:           request.Actor,
			Reviewer:        request.Reviewer,
			ExpectedVersion: request.ExpectedVersion,
			Reason:          reason,
			Standing:        stored.Standing,
		})
		result = DismissResult{Kind: decision.Kind, Reason: decision.Reason, Findings: findings}
		if decision.Kind != memory.SuggestionDismiss {
			return nil
		}
		dismissalID, err := s.newID("review")
		if err != nil {
			return err
		}
		dismissal := records.Review{
			ID:          dismissalID,
			VersionID:   stored.Standing.VersionID,
			ContentHash: stored.Standing.ContentHash,
			Decision:    memory.ReviewDismiss,
			Actor:       request.Actor,
			Reviewer:    request.Reviewer,
			Reason:      reason,
			Suggestion:  decision.Action,
			ReviewedAt:  request.Now.UTC(),
		}
		if err := tx.InsertReview(ctx, dismissal); err != nil {
			return fmt.Errorf("insert memory suggestion dismissal: %w", err)
		}
		result.Action, result.DismissalID = decision.Action, dismissalID
		return nil
	})
	if err != nil {
		return DismissResult{}, fmt.Errorf("dismiss audit suggestion: %w", err)
	}
	return result, nil
}

// redactReason redacts a human's reason; an empty reason stays empty.
func redactReason(reason string) (string, []redaction.Finding) {
	if reason == "" {
		return "", nil
	}
	redacted := policy.Redact(reason)
	return redacted.Text, redacted.Findings
}

func (s Service) newID(prefix string) (string, error) {
	if s.NewID == nil {
		return id.New(prefix)
	}
	return s.NewID(prefix)
}
