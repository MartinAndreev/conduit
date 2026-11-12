package records

import (
	"context"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// CreateRequest describes one record to create. Trust, origin and status
// come from Producer, never from the request.
type CreateRequest struct {
	Producer memory.Producer
	// Actor is established by the presentation entry point for a human, never
	// copied from a request field or an agent binding.
	Actor    memory.Actor
	Author   string
	Category memory.RecordCategory
	Content  string
	// Scope.ProjectID is the record's project.
	Scope   memory.Scope
	Sources []memory.RecordSource
	Now     time.Time
}

// CreateResult reports the decision. RecordID, VersionID, ContentHash and
// Status are set only for memory.CreationAllowed. Findings count what
// redaction replaced, by kind, never the values.
type CreateResult struct {
	Kind        memory.RecordCreationKind
	Reason      string
	RecordID    string
	VersionID   string
	ContentHash string
	Status      memory.RecordStatus
	Findings    []redaction.Finding
}

// CreateRecord redacts the content and sources, decides with
// memory.DecideRecordCreation and, only when allowed, writes the record, its
// first version and sources, and for human text the creator's approval of
// that version, in one transaction. An error before the commit means nothing
// was written; a failed commit may leave the outcome unknown, and a retry
// creates another record.
func (s Service) CreateRecord(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if request.Now.IsZero() {
		return CreateResult{}, ErrInvalidRequest
	}
	redacted := redactMaterial(request.Content, request.Sources)
	var result CreateResult
	err := s.Store.WithinRecordWrite(ctx, func(tx Transaction) error {
		decision := memory.DecideRecordCreation(creationFacts(request, redacted, tx.SearchTextBytes(redacted.content)))
		result = CreateResult{Kind: decision.Kind, Reason: decision.Reason, Findings: redacted.findings}
		if decision.Kind != memory.CreationAllowed {
			return nil
		}
		recordID, err := s.newID("record")
		if err != nil {
			return err
		}
		created, err := s.writeNewRecord(ctx, tx, recordID, request, redacted, decision)
		if err != nil {
			return err
		}
		result.RecordID, result.VersionID, result.ContentHash, result.Status = created.Record.ID, created.Version.ID, created.Version.ContentHash, created.Record.Status
		return nil
	})
	if err != nil {
		return CreateResult{}, fmt.Errorf("create memory record: %w", err)
	}
	return result, nil
}

func creationFacts(request CreateRequest, redacted material, searchTextBytes int64) memory.RecordCreationFacts {
	return memory.RecordCreationFacts{
		Producer:        request.Producer,
		Actor:           request.Actor,
		Author:          request.Author,
		Category:        request.Category,
		Content:         redacted.content,
		SearchTextBytes: searchTextBytes,
		Scope:           request.Scope,
		Sources:         redacted.sources,
	}
}

// writeNewRecord writes an allowed creation: the record with its first
// version and sources, and, when the creator approves, the creator's approve
// review naming that version and its content hash. Human text is thereby
// proposed and approved in this one transaction and commits active.
func (s Service) writeNewRecord(ctx context.Context, tx Transaction, recordID string, request CreateRequest, redacted material, decision memory.RecordCreationDecision) (NewRecord, error) {
	versionID, err := s.newID("version")
	if err != nil {
		return NewRecord{}, err
	}
	now := request.Now.UTC()
	created := NewRecord{
		Record: memory.Record{
			ID:               recordID,
			ProjectID:        request.Scope.ProjectID,
			Category:         request.Category,
			Status:           decision.Status,
			CurrentVersionID: versionID,
			Version:          1,
			CreatedAt:        now,
		},
		Version: memory.RecordVersion{
			ID:                     versionID,
			RecordID:               recordID,
			Content:                redacted.content,
			ContentHash:            memory.ContentHash(redacted.content),
			Author:                 request.Author,
			Trust:                  decision.Trust,
			RedactionPolicyVersion: redacted.policyVersion,
			Scope:                  request.Scope,
		},
		Sources:   redacted.sources,
		CreatedAt: now,
	}
	if err := tx.InsertRecord(ctx, created); err != nil {
		return NewRecord{}, fmt.Errorf("insert memory record: %w", err)
	}
	if !decision.CreatorApproves {
		return created, nil
	}
	reviewID, err := s.newID("review")
	if err != nil {
		return NewRecord{}, err
	}
	approval := Review{
		ID:          reviewID,
		VersionID:   versionID,
		ContentHash: created.Version.ContentHash,
		Decision:    memory.ReviewApprove,
		Actor:       request.Actor,
		Reviewer:    request.Author,
		ReviewedAt:  now,
	}
	if err := tx.InsertReview(ctx, approval); err != nil {
		return NewRecord{}, fmt.Errorf("insert creator approval: %w", err)
	}
	return created, nil
}
