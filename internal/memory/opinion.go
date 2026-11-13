package memory

import (
	"slices"
	"strings"
)

// RecordOpinionFacts describe one opinion about an exact version, read in the
// same write transaction as the review it adds. An opinion annotates an
// immutable version and changes neither trust nor lifecycle, so it does not
// change the record or its optimistic version.
type RecordOpinionFacts struct {
	// RequestProjectID, RequestRecordID, VersionID and ContentHash are what
	// the opinion names.
	RequestProjectID string
	RequestRecordID  string
	VersionID        string
	ContentHash      string
	Actor            Actor
	// Reviewer is the agent or system component giving the opinion.
	Reviewer string
	// Reason is optional without a suggestion and required with one.
	Reason string
	// Suggestion is empty for a plain opinion. An agent's audit opinion
	// suggests archiving or deleting the record (task 5.5).
	Suggestion Suggestion

	// RecordFound is false when no record has the requested ID in any project.
	RecordFound bool
	Record      Record
	// VersionFound is false when the record has no version VersionID.
	VersionFound bool
	// StoredContentHash and ContentDeleted describe the stored version.
	StoredContentHash string
	ContentDeleted    bool
}

type RecordOpinionKind string

const (
	OpinionAdd             RecordOpinionKind = "add"
	OpinionInvalidFacts    RecordOpinionKind = "invalid_facts"
	OpinionActorNotAllowed RecordOpinionKind = "actor_not_allowed"
	// OpinionNotFound covers a missing record, one of another project, and a
	// version that is not the record's.
	OpinionNotFound RecordOpinionKind = "not_found"
	// OpinionHashMismatch: the named content hash is not the version's.
	OpinionHashMismatch RecordOpinionKind = "hash_mismatch"
	// OpinionContentDeleted refuses a reason on a version whose content was
	// deleted, so deleted text cannot return through memory_reviews.reason.
	OpinionContentDeleted RecordOpinionKind = "content_deleted"
	// OpinionReasonTooLong refuses a reason above MaxTextFieldBytes after
	// redaction; it is never truncated.
	OpinionReasonTooLong RecordOpinionKind = "reason_too_long"
	// OpinionReasonRequired refuses a suggestion without a reason.
	OpinionReasonRequired RecordOpinionKind = "reason_required"
	// OpinionVersionNotCurrent refuses a suggestion about a version that is
	// not the record's current one: it could never be confirmed.
	OpinionVersionNotCurrent RecordOpinionKind = "version_not_current"
	// OpinionStatusNotApplicable refuses a suggestion whose action the
	// record's status does not permit (archive needs a status a human may
	// archive).
	OpinionStatusNotApplicable RecordOpinionKind = "status_not_applicable"
)

type RecordOpinionDecision struct {
	Kind   RecordOpinionKind
	Reason string
}

// DecideRecordOpinion answers whether an agent or the system may add an
// opinion about one exact version. A human's decisions are approve and reject
// transitions, not opinions through this path. Only an agent suggests.
//
// Order: invalid request facts (including an unknown suggestion); a human
// actor, or a suggestion from anyone but an agent, before anything about the
// record is revealed; a missing or foreign record or version; a content hash
// that is not the version's; a reason or a suggestion on deleted content; a
// suggestion without a reason; a reason above MaxTextFieldBytes; a
// suggestion about a version that is not the record's current one; a
// suggestion whose action the record's status does not permit; add. An
// opinion without a reason or suggestion on deleted content is added.
func DecideRecordOpinion(f RecordOpinionFacts) RecordOpinionDecision {
	if f.RequestProjectID == "" || f.RequestRecordID == "" || f.VersionID == "" || f.ContentHash == "" ||
		f.Reviewer == "" || len(f.Reviewer) > MaxIdentityBytes || !slices.Contains(Actors(), f.Actor) ||
		(f.Suggestion != "" && !slices.Contains(Suggestions(), f.Suggestion)) {
		return RecordOpinionDecision{Kind: OpinionInvalidFacts, Reason: "opinion needs a project, record, version, content hash, a reviewer within the identity bound, a known actor and a known suggestion"}
	}
	if f.Actor == ActorHuman {
		return RecordOpinionDecision{Kind: OpinionActorNotAllowed, Reason: "only an agent or the system adds an opinion here"}
	}
	if f.Suggestion != "" && f.Actor != ActorAgent {
		return RecordOpinionDecision{Kind: OpinionActorNotAllowed, Reason: "only an agent suggests archiving or deleting"}
	}
	if !f.RecordFound || f.Record.ID != f.RequestRecordID || f.Record.ProjectID != f.RequestProjectID || !f.VersionFound {
		return RecordOpinionDecision{Kind: OpinionNotFound, Reason: "the project has no such record version"}
	}
	if f.StoredContentHash != f.ContentHash {
		return RecordOpinionDecision{Kind: OpinionHashMismatch, Reason: "opinion must name the version's content hash"}
	}
	if f.ContentDeleted && (f.Reason != "" || f.Suggestion != "") {
		return RecordOpinionDecision{Kind: OpinionContentDeleted, Reason: "the version's content was deleted; an opinion on it carries no reason or suggestion"}
	}
	if f.Suggestion != "" && strings.TrimSpace(f.Reason) == "" {
		return RecordOpinionDecision{Kind: OpinionReasonRequired, Reason: "a suggestion needs a reason"}
	}
	if len(f.Reason) > MaxTextFieldBytes {
		return RecordOpinionDecision{Kind: OpinionReasonTooLong, Reason: "the reason exceeds the text field bound"}
	}
	if f.Suggestion != "" && f.VersionID != f.Record.CurrentVersionID {
		return RecordOpinionDecision{Kind: OpinionVersionNotCurrent, Reason: "a suggestion names the record's current version"}
	}
	if f.Suggestion != "" && !suggestionApplies(f.Suggestion, f.Record.Status) {
		return RecordOpinionDecision{Kind: OpinionStatusNotApplicable, Reason: "the record's status does not permit the suggested action"}
	}
	return RecordOpinionDecision{Kind: OpinionAdd, Reason: "opinion names an existing version and its content hash"}
}
