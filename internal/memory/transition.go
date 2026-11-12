package memory

import (
	"slices"
	"strings"
	"time"
)

// proposalRetention is how long a proposal may wait for a decision.
const proposalRetention = 90 * 24 * time.Hour

// ProposalRetentionCutoff is the latest created_at a proposal may have to be
// archived at now. A sweep may select candidates by it; the transition rule
// still decides each one.
func ProposalRetentionCutoff(now time.Time) time.Time {
	return now.Add(-proposalRetention)
}

// SourceVerification is one declared file source checked by a human: its path,
// its current hash, and the hash the human verified.
type SourceVerification struct {
	Path         string
	CurrentHash  string
	VerifiedHash string
}

// RecordTransitionFacts must be read in the same transaction as the write that
// follows an allowed decision.
type RecordTransitionFacts struct {
	// RequestProjectID is the project the request names.
	RequestProjectID string
	// RecordFound is false when no record has the requested ID in any project.
	RecordFound        bool
	Record             Record
	CurrentContentHash string
	// CurrentContentDeleted is true when the current version's content was
	// deleted (content_deleted_at is set).
	CurrentContentDeleted bool
	To                    RecordStatus
	// Actor is established by the presentation entry point for a human; a
	// request field or an agent binding cannot claim it.
	Actor           Actor
	ExpectedVersion int64

	// ApprovedVersionID and ApprovedContentHash are what the approval names.
	ApprovedVersionID   string
	ApprovedContentHash string
	// Reason is the human's stated reason for a rejection or archive.
	Reason string

	Now time.Time
	// HumanDecisionRecorded is true when a human already decided the proposal.
	HumanDecisionRecorded bool

	// SourceApplicability is DecideApplicability's result for the linked sources.
	SourceApplicability ApplicabilityKind

	// DeclaredSources is the record's complete declared source set.
	DeclaredSources []RecordSource
	Verifications   []SourceVerification
	// NewVersionID, NewVersionContentHash and NewVersionSources describe the
	// version a reactivation creates with the verified hashes.
	NewVersionID          string
	NewVersionContentHash string
	NewVersionSources     []RecordSource

	ReplacementRecordID string
	// ReplacementBecomesActive is true when the new record turns active in this
	// same transaction.
	ReplacementBecomesActive bool
}

type RecordTransitionKind string

const (
	TransitionAllowed      RecordTransitionKind = "allowed"
	TransitionInvalidFacts RecordTransitionKind = "invalid_facts"
	// TransitionNotFound covers both a missing record and one of another
	// project, so a request cannot learn about other projects' records.
	TransitionNotFound        RecordTransitionKind = "not_found"
	TransitionVersionConflict RecordTransitionKind = "version_conflict"
	TransitionNotPermitted    RecordTransitionKind = "not_permitted"
	TransitionActorNotAllowed RecordTransitionKind = "actor_not_allowed"
	// TransitionContentDeleted refuses a transition that would write onto a
	// version whose content was deleted: an approval, a reason, or a new
	// version copied from it. Otherwise deleted text could return through a
	// review reason or source, and nobody can review text that is gone.
	TransitionContentDeleted      RecordTransitionKind = "content_deleted"
	TransitionApprovalMismatch    RecordTransitionKind = "approval_mismatch"
	TransitionReasonRequired      RecordTransitionKind = "reason_required"
	TransitionRetentionNotElapsed RecordTransitionKind = "retention_not_elapsed"
	TransitionDecisionRecorded    RecordTransitionKind = "decision_recorded"
	TransitionSourceNotMismatched RecordTransitionKind = "source_not_mismatched"
	TransitionVerificationMissing RecordTransitionKind = "verification_missing"
	TransitionReplacementMissing  RecordTransitionKind = "replacement_missing"
)

type RecordTransitionDecision struct {
	Kind   RecordTransitionKind
	Reason string
}

type transitionRule struct {
	actor Actor
	// writesOnCurrentVersion is true when the transition writes a review onto
	// the current version or copies it into a new one.
	writesOnCurrentVersion bool
	precondition           func(RecordTransitionFacts) RecordTransitionDecision
}

// transitionTable is the lifecycle table from the spec, keyed by {from, to}.
// An approval writes an approve review, a rejection a reject review with its
// reason, a human archive an opinion review with its reason, and a
// reactivation a new version copied from the current one.
var transitionTable = map[[2]RecordStatus]transitionRule{
	{StatusProposed, StatusActive}:              {ActorHuman, true, approvalNamesCurrentVersion},
	{StatusProposed, StatusRejected}:            {ActorHuman, true, reasonGiven},
	{StatusProposed, StatusArchived}:            {ActorSystem, false, retentionElapsed},
	{StatusActive, StatusNeedsVerification}:     {ActorSystem, false, sourceMismatched},
	{StatusNeedsVerification, StatusActive}:     {ActorHuman, true, sourcesVerified},
	{StatusActive, StatusSuperseded}:            {ActorHuman, false, replacementActivated},
	{StatusNeedsVerification, StatusSuperseded}: {ActorHuman, false, replacementActivated},
	{StatusActive, StatusArchived}:              {ActorHuman, true, reasonGiven},
	{StatusNeedsVerification, StatusArchived}:   {ActorHuman, true, reasonGiven},
}

// DecideRecordTransition answers whether a record may move to the requested
// status.
//
// Order: invalid request facts; a missing or foreign record; invalid record
// facts; a stale expected version, which precedes every other refusal because
// the remaining facts were read against a record that has since changed; a
// transition the lifecycle does not have; the wrong actor; a write onto
// deleted content; the transition's precondition.
func DecideRecordTransition(f RecordTransitionFacts) RecordTransitionDecision {
	rule, decision := precheckTransition(f)
	if decision.Kind != TransitionAllowed {
		return decision
	}
	return rule.precondition(f)
}

// PrecheckRecordTransition answers every check of DecideRecordTransition that
// precedes the transition's precondition, in the same order. Allowed means
// only that the precondition remains to be decided: a procedure whose
// precondition facts need I/O (a snapshot read) calls this first, gathers
// those facts only when it allows, and then calls DecideRecordTransition.
func PrecheckRecordTransition(f RecordTransitionFacts) RecordTransitionDecision {
	_, decision := precheckTransition(f)
	return decision
}

func precheckTransition(f RecordTransitionFacts) (transitionRule, RecordTransitionDecision) {
	if f.RequestProjectID == "" || !validStatus(f.To) || !slices.Contains(Actors(), f.Actor) || f.ExpectedVersion <= 0 {
		return transitionRule{}, RecordTransitionDecision{Kind: TransitionInvalidFacts, Reason: "request needs a project, a known target status and actor, and a positive expected version"}
	}
	if !f.RecordFound || f.Record.ProjectID != f.RequestProjectID {
		return transitionRule{}, RecordTransitionDecision{Kind: TransitionNotFound, Reason: "the project has no such record"}
	}
	if !validStatus(f.Record.Status) || f.Record.Version <= 0 {
		return transitionRule{}, RecordTransitionDecision{Kind: TransitionInvalidFacts, Reason: "record status or version is missing or unknown"}
	}
	if f.ExpectedVersion != f.Record.Version {
		return transitionRule{}, RecordTransitionDecision{Kind: TransitionVersionConflict, Reason: "expected version differs from the record's version"}
	}
	rule, permitted := transitionTable[[2]RecordStatus{f.Record.Status, f.To}]
	if !permitted {
		if f.Record.Status.Terminal() {
			return transitionRule{}, RecordTransitionDecision{Kind: TransitionNotPermitted, Reason: "terminal record cannot change; restoring means a new record"}
		}
		return transitionRule{}, RecordTransitionDecision{Kind: TransitionNotPermitted, Reason: "lifecycle has no such transition"}
	}
	if f.Actor != rule.actor {
		return transitionRule{}, RecordTransitionDecision{Kind: TransitionActorNotAllowed, Reason: "transition requires actor " + string(rule.actor)}
	}
	if rule.writesOnCurrentVersion && f.CurrentContentDeleted {
		return transitionRule{}, RecordTransitionDecision{Kind: TransitionContentDeleted, Reason: "the current version's content was deleted; nothing is written onto it"}
	}
	return rule, RecordTransitionDecision{Kind: TransitionAllowed, Reason: "only the transition's precondition remains"}
}

func allowedTransition() RecordTransitionDecision {
	return RecordTransitionDecision{Kind: TransitionAllowed, Reason: "transition precondition holds"}
}

func approvalNamesCurrentVersion(f RecordTransitionFacts) RecordTransitionDecision {
	if f.ApprovedVersionID == "" || f.ApprovedContentHash == "" ||
		f.ApprovedVersionID != f.Record.CurrentVersionID || f.ApprovedContentHash != f.CurrentContentHash {
		return RecordTransitionDecision{Kind: TransitionApprovalMismatch, Reason: "approval must name the exact current version and content hash"}
	}
	return allowedTransition()
}

func reasonGiven(f RecordTransitionFacts) RecordTransitionDecision {
	if strings.TrimSpace(f.Reason) == "" {
		return RecordTransitionDecision{Kind: TransitionReasonRequired, Reason: "transition requires a stated reason"}
	}
	return allowedTransition()
}

func retentionElapsed(f RecordTransitionFacts) RecordTransitionDecision {
	if f.Now.IsZero() || f.Record.CreatedAt.IsZero() {
		return RecordTransitionDecision{Kind: TransitionInvalidFacts, Reason: "retention needs the current time and created_at"}
	}
	if f.HumanDecisionRecorded {
		return RecordTransitionDecision{Kind: TransitionDecisionRecorded, Reason: "proposal already has a decision"}
	}
	if f.Now.Sub(f.Record.CreatedAt) < proposalRetention {
		return RecordTransitionDecision{Kind: TransitionRetentionNotElapsed, Reason: "90 days from created_at have not passed"}
	}
	return allowedTransition()
}

func sourceMismatched(f RecordTransitionFacts) RecordTransitionDecision {
	switch f.SourceApplicability {
	case ApplicabilityMismatch, ApplicabilitySourceUnavailable:
		return allowedTransition()
	default:
		return RecordTransitionDecision{Kind: TransitionSourceNotMismatched, Reason: "no linked source differs or is unavailable"}
	}
}

// sourcesVerified requires exactly one verification per declared file source. A
// hash back at the declared value is as valid as a new one, but reactivation
// always creates a new version carrying the verified hashes, and the approval
// must name that version and its content hash.
func sourcesVerified(f RecordTransitionFacts) RecordTransitionDecision {
	verified, complete := verifiedFileHashes(f.DeclaredSources, f.Verifications)
	if !complete {
		return RecordTransitionDecision{Kind: TransitionVerificationMissing, Reason: "every declared file source must be unique and verified once against its current hash"}
	}
	if f.NewVersionID == "" || f.NewVersionID == f.Record.CurrentVersionID || f.ApprovedVersionID != f.NewVersionID ||
		f.ApprovedContentHash == "" || f.ApprovedContentHash != f.NewVersionContentHash {
		return RecordTransitionDecision{Kind: TransitionApprovalMismatch, Reason: "approval must name the new version and its content hash"}
	}
	if !sourcesCarryVerifiedHashes(f.NewVersionSources, verified) {
		return RecordTransitionDecision{Kind: TransitionApprovalMismatch, Reason: "new version must carry exactly the declared file paths with their verified hashes"}
	}
	return allowedTransition()
}

// verifiedFileHashes returns the verified hash by declared file path. It
// reports false for a duplicate or empty declared path, or any verification
// that is missing, repeated, unrelated, or not against the current hash.
func verifiedFileHashes(declared []RecordSource, verifications []SourceVerification) (map[string]string, bool) {
	declaredPaths := make(map[string]bool)
	for _, source := range declared {
		if source.Kind != SourceFile {
			continue
		}
		if source.Path == "" || declaredPaths[source.Path] {
			return nil, false
		}
		declaredPaths[source.Path] = true
	}
	if len(declaredPaths) == 0 || len(verifications) != len(declaredPaths) {
		return nil, false
	}
	verified := make(map[string]string, len(verifications))
	for _, verification := range verifications {
		if _, repeated := verified[verification.Path]; repeated || !declaredPaths[verification.Path] ||
			verification.CurrentHash == "" || verification.VerifiedHash != verification.CurrentHash {
			return nil, false
		}
		verified[verification.Path] = verification.VerifiedHash
	}
	return verified, true
}

// sourcesCarryVerifiedHashes consumes verified, so a repeated path fails.
func sourcesCarryVerifiedHashes(sources []RecordSource, verified map[string]string) bool {
	for _, source := range sources {
		if source.Kind != SourceFile {
			continue
		}
		hash, pending := verified[source.Path]
		if !pending || source.BlobHash != hash {
			return false
		}
		delete(verified, source.Path)
	}
	return len(verified) == 0
}

func replacementActivated(f RecordTransitionFacts) RecordTransitionDecision {
	if f.ReplacementRecordID == "" || f.ReplacementRecordID == f.Record.ID || !f.ReplacementBecomesActive {
		return RecordTransitionDecision{Kind: TransitionReplacementMissing, Reason: "a new record must become active in the same transaction"}
	}
	return allowedTransition()
}
