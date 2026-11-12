package records

import (
	"errors"
	"time"

	"github.com/MartinAndreev/conduit/internal/id"
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
)

// Service exposes the record write procedures.
type Service struct {
	Store Store
	// Snapshot observes the target workspace for MarkNeedsVerification and
	// Reactivate.
	Snapshot compose.SnapshotReader
	// NewID creates record, version and review IDs; nil means id.New.
	NewID func(prefix string) (string, error)
}

// ErrInvalidRequest reports a request without the current time or, where a
// review is written, without a reviewer. Other invalid fields are a rule's
// invalid_facts decision.
var ErrInvalidRequest = errors.New("memory record request needs the current time and, for a review, a reviewer")

// policy redacts every text before it is persisted: content, source refs and
// paths, and review reasons (spec 012 "Redaction").
var policy redaction.Redactor = redaction.Policy{}

func (s Service) newID(prefix string) (string, error) {
	if s.NewID == nil {
		return id.New(prefix)
	}
	return s.NewID(prefix)
}

func validRequest(now time.Time, reviewer string) error {
	if now.IsZero() || reviewer == "" {
		return ErrInvalidRequest
	}
	return nil
}

// material is content and sources after redaction, with the findings of all
// of it and the policy version that produced them.
type material struct {
	content       string
	sources       []memory.RecordSource
	findings      []redaction.Finding
	policyVersion string
}

// redactMaterial redacts the content and every source ref and path. Blob
// hashes are machine values and stay as given.
func redactMaterial(content string, sources []memory.RecordSource) material {
	result := policy.Redact(content)
	out := material{content: result.Text, findings: result.Findings, policyVersion: result.PolicyVersion}
	for _, source := range sources {
		ref, path := policy.Redact(source.Ref), policy.Redact(source.Path)
		out.findings = redaction.MergeFindings(out.findings, ref.Findings, path.Findings)
		source.Ref, source.Path = ref.Text, path.Text
		out.sources = append(out.sources, source)
	}
	return out
}

// redactReason redacts a review reason; an empty reason stays empty.
func redactReason(reason string) (string, []redaction.Finding) {
	if reason == "" {
		return "", nil
	}
	result := policy.Redact(reason)
	return result.Text, result.Findings
}

// transitionFacts are the facts every transition reads; the procedure adds
// its own precondition facts.
func transitionFacts(projectID string, expectedVersion int64, to memory.RecordStatus, actor memory.Actor, stored RecordFacts, now time.Time) memory.RecordTransitionFacts {
	return memory.RecordTransitionFacts{
		RequestProjectID:      projectID,
		RecordFound:           stored.Found,
		Record:                stored.Record,
		CurrentContentHash:    stored.CurrentContentHash,
		CurrentContentDeleted: stored.CurrentContentDeleted,
		To:                    to,
		Actor:                 actor,
		ExpectedVersion:       expectedVersion,
		Now:                   now,
		HumanDecisionRecorded: stored.HumanDecisionRecorded,
		DeclaredSources:       stored.Sources,
	}
}

// statusChange moves the record read in stored to the decided status.
func statusChange(stored RecordFacts, expectedVersion int64, to memory.RecordStatus, now time.Time) StatusChange {
	return StatusChange{
		ProjectID:       stored.Record.ProjectID,
		RecordID:        stored.Record.ID,
		ExpectedVersion: expectedVersion,
		From:            stored.Record.Status,
		To:              to,
		UpdatedAt:       now.UTC(),
	}
}

// TransitionResult reports a transition decision. RecordVersion, the
// record's new optimistic version, is set only for an allowed transition;
// VersionID only for a reactivation's new version. Findings count what
// redaction replaced in the request's text, never the values.
type TransitionResult struct {
	Kind          memory.RecordTransitionKind
	Reason        string
	RecordVersion int64
	VersionID     string
	Findings      []redaction.Finding
}
