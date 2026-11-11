package memory

import (
	"slices"
	"time"
)

// DeletionSubjectKind is what a context_deletion_receipts row records the
// deletion of.
type DeletionSubjectKind string

const (
	// DeletionSubjectMemoryVersion is the content of one memory record version.
	DeletionSubjectMemoryVersion DeletionSubjectKind = "memory_version"
	// DeletionSubjectManifestPayload is one manifest's payload text.
	DeletionSubjectManifestPayload DeletionSubjectKind = "manifest_payload"
)

// DeletionSubjectKinds lists every deletion receipt subject kind.
func DeletionSubjectKinds() []DeletionSubjectKind {
	return []DeletionSubjectKind{DeletionSubjectMemoryVersion, DeletionSubjectManifestPayload}
}

// DeletionReceipt is one append-only context_deletion_receipts row. It names
// what was deleted and the deleted text's hash, never the text.
type DeletionReceipt struct {
	ID          string
	ProjectID   string
	SubjectKind DeletionSubjectKind
	SubjectID   string
	ContentHash string
	DeletedAt   time.Time
}

// VersionDeletionState is one version of a record as the deletion reads it.
type VersionDeletionState struct {
	VersionID      string
	ContentHash    string
	ContentDeleted bool
}

// RecordDeletionFacts must be read in the same write transaction as the
// deletion that follows a delete decision.
type RecordDeletionFacts struct {
	// RequestProjectID and RequestRecordID are what the request names.
	RequestProjectID string
	RequestRecordID  string
	ExpectedVersion  int64
	// Actor is established by the presentation entry point; a request field or
	// an agent binding cannot claim human.
	Actor Actor

	// RecordFound is false when no record has the requested ID in any project.
	RecordFound bool
	// Record is the stored memory_records row; Record.Version is its optimistic
	// version.
	Record Record
	// Versions are all of the record's versions, in any order.
	Versions []VersionDeletionState
}

type RecordDeletionKind string

const (
	// DeletionDelete clears the content of Decision.Versions.
	DeletionDelete RecordDeletionKind = "delete"
	// DeletionAlreadyDeleted means every version's content is already deleted;
	// nothing is written.
	DeletionAlreadyDeleted  RecordDeletionKind = "already_deleted"
	DeletionActorNotAllowed RecordDeletionKind = "actor_not_allowed"
	DeletionVersionConflict RecordDeletionKind = "version_conflict"
	// DeletionNotFound covers both a missing record and one of another
	// project, so a request cannot learn about other projects' records.
	DeletionNotFound     RecordDeletionKind = "not_found"
	DeletionInvalidFacts RecordDeletionKind = "invalid_facts"
)

// RecordDeletionDecision carries, only for DeletionDelete, the versions whose
// content is still present, in fact order. The deletion clears them and writes
// one receipt each.
type RecordDeletionDecision struct {
	Kind     RecordDeletionKind
	Reason   string
	Versions []VersionDeletionState
}

// DecideRecordDeletion answers whether a request may delete a record's
// content. The unit of deletion is the whole record: the content of all its
// versions. Only a human deletes.
//
// Order: invalid request facts; a non-human actor, before anything about the
// record is revealed; a missing or foreign record; invalid record facts; a
// record whose versions are all deleted, before the version check, so a retry
// after a successful deletion (whose expected version is now stale) is a
// no-op; a stale expected version; delete. A record where only some versions
// are deleted should not exist, because one deletion clears every version in
// one transaction; if it does, the remaining versions are deleted.
func DecideRecordDeletion(f RecordDeletionFacts) RecordDeletionDecision {
	if f.RequestProjectID == "" || f.RequestRecordID == "" || f.ExpectedVersion < 1 || !slices.Contains(Actors(), f.Actor) {
		return RecordDeletionDecision{Kind: DeletionInvalidFacts, Reason: "request needs a project, a record, a positive expected version and a known actor"}
	}
	if f.Actor != ActorHuman {
		return RecordDeletionDecision{Kind: DeletionActorNotAllowed, Reason: "only a human deletes memory"}
	}
	if !f.RecordFound || f.Record.ProjectID != f.RequestProjectID {
		return RecordDeletionDecision{Kind: DeletionNotFound, Reason: "the project has no such record"}
	}
	pending, valid := pendingVersions(f)
	if !valid {
		return RecordDeletionDecision{Kind: DeletionInvalidFacts, Reason: "record needs its id, a positive version and unique versions with ids and hashes, including the current one"}
	}
	if len(pending) == 0 {
		return RecordDeletionDecision{Kind: DeletionAlreadyDeleted, Reason: "every version's content is already deleted"}
	}
	if f.ExpectedVersion != f.Record.Version {
		return RecordDeletionDecision{Kind: DeletionVersionConflict, Reason: "expected version differs from the record's version"}
	}
	if len(pending) < len(f.Versions) {
		return RecordDeletionDecision{Kind: DeletionDelete, Reason: "record is partially deleted; the remaining versions are deleted", Versions: pending}
	}
	return RecordDeletionDecision{Kind: DeletionDelete, Reason: "a human deletes the record's content", Versions: pending}
}

// pendingVersions returns the versions whose content is present and whether
// the record facts are valid.
func pendingVersions(f RecordDeletionFacts) ([]VersionDeletionState, bool) {
	if f.Record.ID != f.RequestRecordID || f.Record.Version < 1 || len(f.Versions) == 0 {
		return nil, false
	}
	seen := make(map[string]bool, len(f.Versions))
	var pending []VersionDeletionState
	for _, version := range f.Versions {
		if version.VersionID == "" || version.ContentHash == "" || seen[version.VersionID] {
			return nil, false
		}
		seen[version.VersionID] = true
		if !version.ContentDeleted {
			pending = append(pending, version)
		}
	}
	if !seen[f.Record.CurrentVersionID] {
		return nil, false
	}
	return pending, true
}

// PayloadStatus is a context_manifest_payloads row's status.
type PayloadStatus string

const (
	PayloadPresent                PayloadStatus = "present"
	PayloadRemovedBeforeDispatch  PayloadStatus = "removed_before_dispatch"
	PayloadRemovedDeliveryUnknown PayloadStatus = "removed_delivery_unknown"
	PayloadRemovedAfterDelivery   PayloadStatus = "removed_after_delivery"
)

// PayloadStatuses lists every payload status.
func PayloadStatuses() []PayloadStatus {
	return []PayloadStatus{PayloadPresent, PayloadRemovedBeforeDispatch, PayloadRemovedDeliveryUnknown, PayloadRemovedAfterDelivery}
}

// DispatchState is how far the manifest's dispatch got when its payload is
// removed. It is read from the dispatch intent and outcome of spec 003.
type DispatchState string

const (
	// DispatchIntentRecorded: the intent is recorded and no send was attempted.
	DispatchIntentRecorded DispatchState = "intent_recorded"
	// DispatchDeliveryUnknown: a send was attempted and its outcome is unknown.
	DispatchDeliveryUnknown DispatchState = "delivery_unknown"
	// DispatchDelivered: the provider reported delivery.
	DispatchDelivered DispatchState = "delivered"
)

// DispatchStates lists every dispatch state the payload rule knows.
func DispatchStates() []DispatchState {
	return []DispatchState{DispatchIntentRecorded, DispatchDeliveryUnknown, DispatchDelivered}
}

// IntentEffect is what removing a payload requires of its dispatch intent.
type IntentEffect string

const (
	// IntentInvalidate: the intent is invalidated in the same transaction as
	// the removal; a later dispatch needs a new intent with a new manifest.
	IntentInvalidate IntentEffect = "invalidate"
	// IntentForbidRetry: retrying the same intent is forbidden, since the first
	// send may have been delivered.
	IntentForbidRetry IntentEffect = "forbid_retry"
	// IntentUnchanged: the intent completed; nothing changes.
	IntentUnchanged IntentEffect = "unchanged"
)

type PayloadRemovalFacts struct {
	// Status is the payload's stored status.
	Status   PayloadStatus
	Dispatch DispatchState
}

type PayloadRemovalKind string

const (
	// PayloadRemove removes the text and sets Decision.Status.
	PayloadRemove PayloadRemovalKind = "remove"
	// PayloadAlreadyRemoved: the text is already gone; nothing is written.
	PayloadAlreadyRemoved PayloadRemovalKind = "already_removed"
	PayloadInvalidFacts   PayloadRemovalKind = "invalid_facts"
)

// PayloadRemovalDecision carries Status and Intent only for PayloadRemove.
type PayloadRemovalDecision struct {
	Kind   PayloadRemovalKind
	Reason string
	Status PayloadStatus
	Intent IntentEffect
}

// DecidePayloadRemoval chooses the status a present payload takes when its
// text is removed, from the dispatch state at that moment. An unknown status
// or dispatch state is invalid, including for a payload already removed.
func DecidePayloadRemoval(f PayloadRemovalFacts) PayloadRemovalDecision {
	if !slices.Contains(PayloadStatuses(), f.Status) || !slices.Contains(DispatchStates(), f.Dispatch) {
		return PayloadRemovalDecision{Kind: PayloadInvalidFacts, Reason: "payload status or dispatch state is unknown"}
	}
	if f.Status != PayloadPresent {
		return PayloadRemovalDecision{Kind: PayloadAlreadyRemoved, Reason: "payload text is already removed"}
	}
	switch f.Dispatch {
	case DispatchIntentRecorded:
		return PayloadRemovalDecision{Kind: PayloadRemove, Reason: "not dispatched: the intent is invalidated with the removal", Status: PayloadRemovedBeforeDispatch, Intent: IntentInvalidate}
	case DispatchDeliveryUnknown:
		return PayloadRemovalDecision{Kind: PayloadRemove, Reason: "delivery unknown: retrying the same intent is forbidden", Status: PayloadRemovedDeliveryUnknown, Intent: IntentForbidRetry}
	default:
		return PayloadRemovalDecision{Kind: PayloadRemove, Reason: "delivered: only the stored copy is removed", Status: PayloadRemovedAfterDelivery, Intent: IntentUnchanged}
	}
}
