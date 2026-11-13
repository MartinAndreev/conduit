package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// MaxPendingPageSize is the most stored suggestions one pending read
// examines.
const MaxPendingPageSize = 100

// ErrInvalidPendingRequest reports a pending request without a project or
// with a page size outside 1..MaxPendingPageSize.
var ErrInvalidPendingRequest = errors.New("pending suggestions request needs a project and a page size from 1 to the maximum")

// SuggestionPosition is a stored suggestion's place in (reviewed_at, review
// id) order.
type SuggestionPosition struct {
	ReviewedAt time.Time
	ReviewID   string
}

// StoredSuggestion is one stored review with its record, as the suggestion
// rules read it. Standing.RequestProjectID is the caller's to set.
type StoredSuggestion struct {
	ReviewID   string
	Reviewer   string
	Reason     string
	ReviewedAt time.Time
	Standing   memory.SuggestionStandingFacts
}

// SuggestionPage is one read of a project's stored agent suggestions.
type SuggestionPage struct {
	Suggestions []StoredSuggestion
	More        bool
}

// SuggestionReader reads the agent suggestions of a project. It only reads;
// the rules decide which are pending.
type SuggestionReader interface {
	// ReadAgentSuggestions reads, in one consistent snapshot, at most limit
	// reviews carrying a suggestion about versions of the project's records,
	// whatever their standing, strictly after the position after in
	// (reviewed_at, review id) order, in that order, each with its record's
	// current state; and whether more follow. A zero after starts at the
	// oldest.
	ReadAgentSuggestions(ctx context.Context, projectID string, after SuggestionPosition, limit int) (SuggestionPage, error)
}

// PendingRequest asks for one page after a position.
type PendingRequest struct {
	ProjectID string
	After     SuggestionPosition
	Limit     int
}

// PendingSuggestion is one suggestion a human may confirm. Record.Version is
// the expected version a confirmation names.
type PendingSuggestion struct {
	ReviewID    string
	Action      memory.Suggestion
	Reason      string
	Reviewer    string
	ReviewedAt  time.Time
	VersionID   string
	ContentHash string
	Record      memory.Record
}

// Pending is one page. A page may hold fewer suggestions than its size, or
// none, when stored suggestions no longer apply. Next continues after the
// last suggestion read, pending or not, and is zero at the end.
type Pending struct {
	Suggestions []PendingSuggestion
	Next        SuggestionPosition
}

// PendingSuggestions reads one page of the project's stored agent
// suggestions and keeps those memory.DecideSuggestionStanding finds still
// applying: the suggestion names the record's current version and content
// hash, the content is not deleted and the status still permits the action.
// Stale suggestions are not listed; nothing about them is stored, and no
// dismissal exists yet.
func (s Service) PendingSuggestions(ctx context.Context, request PendingRequest) (Pending, error) {
	if request.ProjectID == "" || request.Limit < 1 || request.Limit > MaxPendingPageSize {
		return Pending{}, ErrInvalidPendingRequest
	}
	page, err := s.Suggestions.ReadAgentSuggestions(ctx, request.ProjectID, request.After, request.Limit)
	if err != nil {
		return Pending{}, fmt.Errorf("read agent suggestions: %w", err)
	}
	var pending Pending
	for _, stored := range page.Suggestions {
		stored.Standing.RequestProjectID = request.ProjectID
		decision := memory.DecideSuggestionStanding(stored.Standing)
		if decision.Kind != memory.SuggestionApplies {
			continue
		}
		pending.Suggestions = append(pending.Suggestions, PendingSuggestion{
			ReviewID:    stored.ReviewID,
			Action:      decision.Action,
			Reason:      stored.Reason,
			Reviewer:    stored.Reviewer,
			ReviewedAt:  stored.ReviewedAt,
			VersionID:   stored.Standing.VersionID,
			ContentHash: stored.Standing.ContentHash,
			Record:      stored.Standing.Record,
		})
	}
	if page.More && len(page.Suggestions) > 0 {
		last := page.Suggestions[len(page.Suggestions)-1]
		pending.Next = SuggestionPosition{ReviewedAt: last.ReviewedAt, ReviewID: last.ReviewID}
	}
	return pending, nil
}
