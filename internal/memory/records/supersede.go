package records

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// SupersedeRequest is a human replacing an active or stale record with new
// text the human wrote.
type SupersedeRequest struct {
	ProjectID       string
	RecordID        string
	ExpectedVersion int64
	// Actor is established by the presentation entry point.
	Actor memory.Actor
	// Author writes the replacement and approves it.
	Author   string
	Category memory.RecordCategory
	Content  string
	Sources  []memory.RecordSource
	Now      time.Time
}

// SupersedeResult reports both decisions: the replacement's creation and the
// old record's transition. Replacement, ReplacementVersionID, ContentHash and
// RecordVersion (the old record's new optimistic version) are set only when
// both are allowed. Findings count what redaction replaced in the
// replacement, by kind.
type SupersedeResult struct {
	Creation             memory.RecordCreationDecision
	Transition           memory.RecordTransitionDecision
	Replacement          string
	ReplacementVersionID string
	ContentHash          string
	RecordVersion        int64
	Findings             []redaction.Finding
}

// Supersede creates the replacement as human text (proposed and approved by
// the same human, so active) and moves the old record to superseded, in one
// transaction, with a supersedes relation from the replacement to the old
// record. The replacement is always a new record in the old record's project;
// nothing is overwritten. Both memory.DecideRecordCreation and
// memory.DecideRecordTransition are evaluated; anything is written only when
// both allow it.
func (s Service) Supersede(ctx context.Context, request SupersedeRequest) (SupersedeResult, error) {
	if err := validRequest(request.Now, request.Author); err != nil {
		return SupersedeResult{}, err
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
	var result SupersedeResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		stored, err := tx.ReadRecord(ctx, request.RecordID)
		if err != nil {
			return fmt.Errorf("read memory record: %w", err)
		}
		created := memory.DecideRecordCreation(creationFacts(creation, redacted, tx.SearchTextBytes(redacted.content)))
		facts := transitionFacts(request.ProjectID, request.ExpectedVersion, memory.StatusSuperseded, request.Actor, stored, request.Now)
		// A refused creation leaves the transition without a replacement.
		if created.Kind == memory.CreationAllowed {
			if facts.ReplacementRecordID, err = s.newID("record"); err != nil {
				return err
			}
			facts.ReplacementBecomesActive = created.Status == memory.StatusActive
		}
		transition := memory.DecideRecordTransition(facts)
		result = SupersedeResult{Creation: created, Transition: transition, Findings: redacted.findings}
		if created.Kind != memory.CreationAllowed || transition.Kind != memory.TransitionAllowed {
			return nil
		}
		replacement, err := s.writeNewRecord(ctx, tx, facts.ReplacementRecordID, creation, redacted, created)
		if err != nil {
			return err
		}
		if err := tx.ChangeStatus(ctx, statusChange(stored, request.ExpectedVersion, memory.StatusSuperseded, request.Now)); err != nil {
			return fmt.Errorf("change memory record status: %w", err)
		}
		relation := Relation{
			ProjectID:    request.ProjectID,
			FromRecordID: replacement.Record.ID,
			ToRecordID:   stored.Record.ID,
			Kind:         memory.RelationSupersedes,
			CreatedAt:    request.Now.UTC(),
		}
		if err := tx.InsertRelation(ctx, relation); err != nil {
			return fmt.Errorf("insert supersedes relation: %w", err)
		}
		result.Replacement, result.ReplacementVersionID, result.ContentHash = replacement.Record.ID, replacement.Version.ID, replacement.Version.ContentHash
		result.RecordVersion = request.ExpectedVersion + 1
		return nil
	})
	if err != nil {
		return SupersedeResult{}, fmt.Errorf("supersede memory record: %w", err)
	}
	return result, nil
}
