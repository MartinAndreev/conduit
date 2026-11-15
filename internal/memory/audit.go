package memory

import (
	"slices"
	"strings"
)

// Suggestion is what an agent's audit opinion proposes a human do with a
// record (task 5.5). The list is fixed and owned here; storage's CHECK on
// memory_reviews.suggestion mirrors it.
type Suggestion string

const (
	// SuggestArchive proposes the human archive transition.
	SuggestArchive Suggestion = "archive"
	// SuggestDelete proposes the human deletion of the record's content.
	SuggestDelete Suggestion = "delete"
)

// Suggestions lists every suggestion kind.
func Suggestions() []Suggestion {
	return []Suggestion{SuggestArchive, SuggestDelete}
}

// AuditedStatuses are the statuses of the records an audit agent reviews
// (Martin, 2026-11-13): only knowledge that can reach an agent's context.
func AuditedStatuses() []RecordStatus {
	return []RecordStatus{StatusActive, StatusNeedsVerification}
}

// suggestionApplies reports whether the suggested action can still apply to
// a record in status. Archive follows the lifecycle table: a human may archive
// the status. Delete applies to every status; deleted content is refused
// separately.
func suggestionApplies(suggestion Suggestion, status RecordStatus) bool {
	switch suggestion {
	case SuggestArchive:
		rule, ok := transitionTable[[2]RecordStatus{status, StatusArchived}]
		return ok && rule.actor == ActorHuman
	case SuggestDelete:
		return validStatus(status)
	}
	return false
}

// MaxSuggestionsPerReport is how many suggestions of one audit report are
// kept, in report order (initial value). The rest are dropped and counted.
const MaxSuggestionsPerReport = 25

// MaxSuggestionEntriesExamined is how many entries of one audit block are
// decoded and decided on their merits (initial value). Every later entry is
// dropped as over the limit without being decoded.
const MaxSuggestionEntriesExamined = 100

// SuggestionKeepFacts describe one entry of an audit report's block, decoded
// from the raw report with its reason redacted after decoding, and what the
// entries before it in the same report left.
type SuggestionKeepFacts struct {
	// Position is the entry's 0-based position in the block. An entry at or
	// after MaxSuggestionEntriesExamined is not decoded; its other facts are
	// zero.
	Position int
	// Decoded is false when the entry is not a JSON object holding only the
	// format's fields with their types.
	Decoded bool
	// RecordID, VersionID and ContentHash are machine identities: they are
	// never redacted, only checked against their grammar.
	RecordID    string
	VersionID   string
	ContentHash string
	// IdentityChangedByRedaction is true when redaction would change any of
	// the three identities or reports a finding in one: such a value is a
	// secret, never an identity.
	IdentityChangedByRedaction bool
	Action                     Suggestion
	// Reason is the redacted reason.
	Reason string
	// RepeatsEarlierRecord is true when an earlier entry of the same report,
	// kept or dropped only for the limit, names the same record.
	RepeatsEarlierRecord bool
	// Kept counts the entries of the report kept before this one.
	Kept int
}

type SuggestionKeepKind string

const (
	SuggestionKeep         SuggestionKeepKind = "keep"
	SuggestionInvalidFacts SuggestionKeepKind = "invalid_facts"
	// SuggestionMalformedEntry drops an entry that is not an object of the
	// format's fields: an unknown or repeated field, a wrong type or a
	// non-object.
	SuggestionMalformedEntry SuggestionKeepKind = "malformed_entry"
	// SuggestionInvalidIdentity drops an entry whose record ID, version ID or
	// content hash does not fit its grammar or is changed by redaction.
	SuggestionInvalidIdentity SuggestionKeepKind = "invalid_identity"
	SuggestionUnknownAction   SuggestionKeepKind = "unknown_action"
	SuggestionEmptyReason     SuggestionKeepKind = "empty_reason"
	// SuggestionDuplicate drops an entry naming a record an earlier entry of
	// the same report already named, whatever the action.
	SuggestionDuplicate SuggestionKeepKind = "duplicate"
	// SuggestionOverLimit drops a valid entry after MaxSuggestionsPerReport
	// were kept, and any entry at or after MaxSuggestionEntriesExamined.
	SuggestionOverLimit SuggestionKeepKind = "over_limit"
)

// SuggestionKeepKinds lists every kind in DecideSuggestionKeep's order. Every
// kind but SuggestionKeep drops the entry.
func SuggestionKeepKinds() []SuggestionKeepKind {
	return []SuggestionKeepKind{
		SuggestionInvalidFacts, SuggestionMalformedEntry, SuggestionInvalidIdentity, SuggestionUnknownAction,
		SuggestionEmptyReason, SuggestionDuplicate, SuggestionOverLimit, SuggestionKeep,
	}
}

type SuggestionKeepDecision struct {
	Kind   SuggestionKeepKind
	Reason string
}

// DecideSuggestionKeep answers whether one entry of an audit report's block
// is kept as a suggestion. Reasons never repeat a value from the report.
//
// Order: invalid facts (a negative or impossible kept count, a negative
// position); a position at or after MaxSuggestionEntriesExamined
// (over_limit); an entry that did not decode; a record ID, version ID or
// content hash outside its grammar or changed by redaction; an action that is
// not a Suggestion; an empty reason; a record an earlier entry named; the
// MaxSuggestionsPerReport limit already reached; keep. The reason's storage
// bound is DecideRecordOpinion's, so an over-long reason becomes that
// suggestion's recording refusal. Invalid and repeated entries never take one
// of the kept places.
func DecideSuggestionKeep(f SuggestionKeepFacts) SuggestionKeepDecision {
	switch {
	case f.Kept < 0 || f.Kept > MaxSuggestionsPerReport || f.Position < 0:
		return SuggestionKeepDecision{Kind: SuggestionInvalidFacts, Reason: "kept count is outside the report limit or the position is negative"}
	case f.Position >= MaxSuggestionEntriesExamined:
		return SuggestionKeepDecision{Kind: SuggestionOverLimit, Reason: "the entry is past the entries a report may have examined"}
	case !f.Decoded:
		return SuggestionKeepDecision{Kind: SuggestionMalformedEntry, Reason: "entry is not an object of the audit format's fields"}
	case !ValidIdentity(f.RecordID) || !ValidIdentity(f.VersionID) || !ValidContentHash(f.ContentHash) || f.IdentityChangedByRedaction:
		return SuggestionKeepDecision{Kind: SuggestionInvalidIdentity, Reason: "record id, version id or content hash does not fit its grammar"}
	case !slices.Contains(Suggestions(), f.Action):
		return SuggestionKeepDecision{Kind: SuggestionUnknownAction, Reason: "action is not one of the fixed suggestions"}
	case strings.TrimSpace(f.Reason) == "":
		return SuggestionKeepDecision{Kind: SuggestionEmptyReason, Reason: "a suggestion needs a reason"}
	case f.RepeatsEarlierRecord:
		return SuggestionKeepDecision{Kind: SuggestionDuplicate, Reason: "an earlier entry of the report names the same record"}
	case f.Kept == MaxSuggestionsPerReport:
		return SuggestionKeepDecision{Kind: SuggestionOverLimit, Reason: "the report's suggestion limit is reached"}
	}
	return SuggestionKeepDecision{Kind: SuggestionKeep, Reason: "entry is a valid suggestion within the report limit"}
}

// SuggestionStandingFacts describe one stored review, read by its ID, and the
// record of the version it names, read in one snapshot or transaction.
type SuggestionStandingFacts struct {
	// RequestProjectID is the project the request names.
	RequestProjectID string

	// ReviewFound is false when no review has the requested ID.
	ReviewFound bool
	Actor       Actor
	Decision    ReviewDecision
	// Suggestion is empty for a review without one.
	Suggestion Suggestion
	// VersionID and ContentHash are what the review names.
	VersionID   string
	ContentHash string

	// Dismissed is true when a human dismissal names the review's version,
	// content hash and suggestion.
	Dismissed bool

	// RecordFound is false when the review's version has no record.
	RecordFound           bool
	Record                Record
	CurrentContentHash    string
	CurrentContentDeleted bool
}

type SuggestionKind string

const (
	// SuggestionApplies: the suggestion still applies to the record.
	SuggestionApplies      SuggestionKind = "applies"
	SuggestionInvalidInput SuggestionKind = "invalid_facts"
	// SuggestionActorNotAllowed: only a human confirms a suggestion.
	SuggestionActorNotAllowed SuggestionKind = "actor_not_allowed"
	// SuggestionNotFound covers a missing review, a review that is not an
	// agent opinion with a known suggestion, and a record that is missing or
	// of another project.
	SuggestionNotFound SuggestionKind = "not_found"
	// SuggestionStale: the record's current version is no longer the one the
	// suggestion names.
	SuggestionStale SuggestionKind = "stale"
	// SuggestionDismissed: a human dismissed the suggested action for the
	// version and content hash the suggestion names.
	SuggestionDismissed SuggestionKind = "dismissed"
	// SuggestionContentDeleted: the record's content was deleted.
	SuggestionContentDeleted SuggestionKind = "content_deleted"
	// SuggestionStatusMoved: the record's status no longer permits the
	// suggested action.
	SuggestionStatusMoved SuggestionKind = "status_moved"
)

// SuggestionDecision carries, only for SuggestionApplies, the suggested
// action.
type SuggestionDecision struct {
	Kind   SuggestionKind
	Reason string
	Action Suggestion
}

// DecideSuggestionStanding answers whether a stored agent suggestion still
// applies: it is pending for a human. Nothing is stored about a suggestion
// that no longer applies; it simply stops being pending.
//
// Order: no request project (invalid_facts); a missing review, one that is
// not an agent opinion with a known suggestion, or a missing or foreign
// record (not_found); a version or content hash that is no longer the
// record's current one (stale); a human dismissal of the same action for the
// same version and content hash (dismissed); deleted content; a status the
// action cannot apply to (status_moved: archive needs a status a human may
// archive, delete any status); applies.
func DecideSuggestionStanding(f SuggestionStandingFacts) SuggestionDecision {
	if f.RequestProjectID == "" {
		return SuggestionDecision{Kind: SuggestionInvalidInput, Reason: "the request needs a project"}
	}
	if !f.ReviewFound || f.Actor != ActorAgent || f.Decision != ReviewOpinion || !slices.Contains(Suggestions(), f.Suggestion) ||
		!f.RecordFound || f.Record.ProjectID != f.RequestProjectID {
		return SuggestionDecision{Kind: SuggestionNotFound, Reason: "the project has no such agent suggestion"}
	}
	if f.VersionID != f.Record.CurrentVersionID || f.ContentHash != f.CurrentContentHash {
		return SuggestionDecision{Kind: SuggestionStale, Reason: "the record has a newer version than the suggestion names"}
	}
	if f.Dismissed {
		return SuggestionDecision{Kind: SuggestionDismissed, Reason: "a human dismissed this action for the version the suggestion names"}
	}
	if f.CurrentContentDeleted {
		return SuggestionDecision{Kind: SuggestionContentDeleted, Reason: "the record's content was deleted"}
	}
	if !suggestionApplies(f.Suggestion, f.Record.Status) {
		return SuggestionDecision{Kind: SuggestionStatusMoved, Reason: "the record's status no longer permits the suggested action"}
	}
	return SuggestionDecision{Kind: SuggestionApplies, Reason: "the suggestion names the record's current version and its status permits the action", Action: f.Suggestion}
}

// SuggestionConfirmationFacts are a human's confirmation of one suggestion
// and the suggestion's standing facts, read in the confirmation's write
// transaction.
type SuggestionConfirmationFacts struct {
	ReviewID string
	// Actor is established by the presentation entry point; a request field
	// or an agent binding cannot claim human.
	Actor    Actor
	Standing SuggestionStandingFacts
}

// DecideSuggestionConfirmation answers whether a human may confirm one
// suggestion. Applies carries the action; the action itself is then decided
// by its own rule (DecideRecordTransition to archived with the human's
// reason, DecideRecordDeletion), which also checks the expected version.
//
// Order: no review ID or an unknown actor (invalid_facts); a non-human
// actor, before anything about the suggestion is revealed; then
// DecideSuggestionStanding.
func DecideSuggestionConfirmation(f SuggestionConfirmationFacts) SuggestionDecision {
	if f.ReviewID == "" || !slices.Contains(Actors(), f.Actor) {
		return SuggestionDecision{Kind: SuggestionInvalidInput, Reason: "confirmation needs a review id and a known actor"}
	}
	if f.Actor != ActorHuman {
		return SuggestionDecision{Kind: SuggestionActorNotAllowed, Reason: "only a human confirms a suggestion"}
	}
	return DecideSuggestionStanding(f.Standing)
}

// SuggestionDismissalFacts are a human's dismissal of one suggestion and the
// suggestion's standing facts, read in the dismissal's write transaction.
type SuggestionDismissalFacts struct {
	ReviewID string
	// Actor is established by the presentation entry point; a request field
	// or an agent binding cannot claim human.
	Actor Actor
	// Reviewer is the person dismissing.
	Reviewer string
	// ExpectedVersion is the record's version the human saw.
	ExpectedVersion int64
	// Reason is the human's optional reason, after redaction.
	Reason   string
	Standing SuggestionStandingFacts
}

type SuggestionDismissalKind string

const (
	// SuggestionDismiss: the human may dismiss the suggestion.
	SuggestionDismiss                  SuggestionDismissalKind = "dismiss"
	SuggestionDismissalInvalidFacts    SuggestionDismissalKind = "invalid_facts"
	SuggestionDismissalActorNotAllowed SuggestionDismissalKind = "actor_not_allowed"
	SuggestionDismissalNotFound        SuggestionDismissalKind = "not_found"
	SuggestionDismissalStale           SuggestionDismissalKind = "stale"
	SuggestionAlreadyDismissed         SuggestionDismissalKind = "already_dismissed"
	SuggestionDismissalContentDeleted  SuggestionDismissalKind = "content_deleted"
	SuggestionDismissalStatusMoved     SuggestionDismissalKind = "status_moved"
	SuggestionDismissalVersionConflict SuggestionDismissalKind = "version_conflict"
	SuggestionDismissalReasonTooLong   SuggestionDismissalKind = "reason_too_long"
)

// SuggestionDismissalKinds lists every kind in DecideSuggestionDismissal's
// order. Every kind but SuggestionDismiss writes nothing.
func SuggestionDismissalKinds() []SuggestionDismissalKind {
	return []SuggestionDismissalKind{
		SuggestionDismissalInvalidFacts, SuggestionDismissalActorNotAllowed, SuggestionDismissalNotFound, SuggestionDismissalStale,
		SuggestionAlreadyDismissed, SuggestionDismissalContentDeleted, SuggestionDismissalStatusMoved,
		SuggestionDismissalVersionConflict, SuggestionDismissalReasonTooLong, SuggestionDismiss,
	}
}

// dismissalKinds maps each standing that refuses a dismissal to the
// dismissal's kind; SuggestionApplies continues to the dismissal's own
// checks.
var dismissalKinds = map[SuggestionKind]SuggestionDismissalKind{
	SuggestionInvalidInput:   SuggestionDismissalInvalidFacts,
	SuggestionNotFound:       SuggestionDismissalNotFound,
	SuggestionStale:          SuggestionDismissalStale,
	SuggestionDismissed:      SuggestionAlreadyDismissed,
	SuggestionContentDeleted: SuggestionDismissalContentDeleted,
	SuggestionStatusMoved:    SuggestionDismissalStatusMoved,
}

// SuggestionDismissalDecision carries, only for SuggestionDismiss, the
// dismissed action.
type SuggestionDismissalDecision struct {
	Kind   SuggestionDismissalKind
	Reason string
	Action Suggestion
}

// DecideSuggestionDismissal answers whether a human may dismiss one pending
// suggestion (Martin, 7а, 2026-11-15). A dismissal binds the action to the
// version and content hash the suggestion names: the suggestion stops being
// pending, a confirmation refuses it, and the agent cannot suggest the same
// action for that version again. A new version lifts it. Only a suggestion
// that still applies can be dismissed, as only one that applies can be
// confirmed.
//
// Order: no review ID, an unknown actor, no reviewer or one above
// MaxIdentityBytes, or an expected version below 1 (invalid_facts); a non-human actor, before anything about the suggestion is
// revealed; then DecideSuggestionStanding (invalid_facts, not_found, stale,
// already_dismissed, content_deleted, status_moved); an expected version
// that is not the record's (version_conflict); a reason above
// MaxTextFieldBytes (reason_too_long; it is optional and never truncated);
// dismiss.
func DecideSuggestionDismissal(f SuggestionDismissalFacts) SuggestionDismissalDecision {
	if f.ReviewID == "" || !slices.Contains(Actors(), f.Actor) || f.Reviewer == "" || len(f.Reviewer) > MaxIdentityBytes || f.ExpectedVersion < 1 {
		return SuggestionDismissalDecision{Kind: SuggestionDismissalInvalidFacts, Reason: "dismissal needs a review id, a known actor, a reviewer within the identity bound and a positive expected version"}
	}
	if f.Actor != ActorHuman {
		return SuggestionDismissalDecision{Kind: SuggestionDismissalActorNotAllowed, Reason: "only a human dismisses a suggestion"}
	}
	standing := DecideSuggestionStanding(f.Standing)
	if standing.Kind != SuggestionApplies {
		return SuggestionDismissalDecision{Kind: dismissalKinds[standing.Kind], Reason: standing.Reason}
	}
	if f.ExpectedVersion != f.Standing.Record.Version {
		return SuggestionDismissalDecision{Kind: SuggestionDismissalVersionConflict, Reason: "the record changed since the version the human saw"}
	}
	if len(f.Reason) > MaxTextFieldBytes {
		return SuggestionDismissalDecision{Kind: SuggestionDismissalReasonTooLong, Reason: "the reason exceeds the text field bound"}
	}
	return SuggestionDismissalDecision{Kind: SuggestionDismiss, Reason: "the suggestion still applies and the human dismisses it", Action: standing.Action}
}
