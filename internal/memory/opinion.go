package memory

import "slices"

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
	// Reason is optional.
	Reason string

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
)

type RecordOpinionDecision struct {
	Kind   RecordOpinionKind
	Reason string
}

// DecideRecordOpinion answers whether an agent or the system may add an
// opinion about one exact version. A human's decisions are approve and reject
// transitions, not opinions through this path.
//
// Order: invalid request facts; a human actor, before anything about the
// record is revealed; a missing or foreign record or version; a content hash
// that is not the version's; a reason on deleted content; add. An opinion
// without a reason on deleted content is added.
func DecideRecordOpinion(f RecordOpinionFacts) RecordOpinionDecision {
	if f.RequestProjectID == "" || f.RequestRecordID == "" || f.VersionID == "" || f.ContentHash == "" ||
		f.Reviewer == "" || !slices.Contains(Actors(), f.Actor) {
		return RecordOpinionDecision{Kind: OpinionInvalidFacts, Reason: "opinion needs a project, record, version, content hash, reviewer and known actor"}
	}
	if f.Actor == ActorHuman {
		return RecordOpinionDecision{Kind: OpinionActorNotAllowed, Reason: "only an agent or the system adds an opinion here"}
	}
	if !f.RecordFound || f.Record.ID != f.RequestRecordID || f.Record.ProjectID != f.RequestProjectID || !f.VersionFound {
		return RecordOpinionDecision{Kind: OpinionNotFound, Reason: "the project has no such record version"}
	}
	if f.StoredContentHash != f.ContentHash {
		return RecordOpinionDecision{Kind: OpinionHashMismatch, Reason: "opinion must name the version's content hash"}
	}
	if f.ContentDeleted && f.Reason != "" {
		return RecordOpinionDecision{Kind: OpinionContentDeleted, Reason: "the version's content was deleted; an opinion on it carries no reason"}
	}
	return RecordOpinionDecision{Kind: OpinionAdd, Reason: "opinion names an existing version and its content hash"}
}
