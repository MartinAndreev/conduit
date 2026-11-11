package memory

import (
	"slices"
	"testing"
)

func deletionFacts() RecordDeletionFacts {
	return RecordDeletionFacts{
		RequestProjectID: "project",
		RequestRecordID:  "record",
		ExpectedVersion:  3,
		Actor:            ActorHuman,
		RecordFound:      true,
		Record:           Record{ID: "record", ProjectID: "project", CurrentVersionID: "v2", Version: 3},
		Versions: []VersionDeletionState{
			{VersionID: "v1", ContentHash: "h1"},
			{VersionID: "v2", ContentHash: "h2"},
		},
	}
}

func TestDecideRecordDeletion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*RecordDeletionFacts)
		want    RecordDeletionKind
		pending []string
	}{
		{"delete every version", func(*RecordDeletionFacts) {}, DeletionDelete, []string{"v1", "v2"}},
		{"partially deleted deletes the rest", func(f *RecordDeletionFacts) { f.Versions[0].ContentDeleted = true }, DeletionDelete, []string{"v2"}},
		{"already deleted", func(f *RecordDeletionFacts) {
			f.Versions[0].ContentDeleted, f.Versions[1].ContentDeleted = true, true
		}, DeletionAlreadyDeleted, nil},
		{"already deleted precedes a stale version", func(f *RecordDeletionFacts) {
			f.Versions[0].ContentDeleted, f.Versions[1].ContentDeleted = true, true
			f.ExpectedVersion = 2
		}, DeletionAlreadyDeleted, nil},
		{"stale expected version", func(f *RecordDeletionFacts) { f.ExpectedVersion = 2 }, DeletionVersionConflict, nil},
		{"agent", func(f *RecordDeletionFacts) { f.Actor = ActorAgent }, DeletionActorNotAllowed, nil},
		{"system", func(f *RecordDeletionFacts) { f.Actor = ActorSystem }, DeletionActorNotAllowed, nil},
		{"agent on a missing record learns nothing", func(f *RecordDeletionFacts) {
			f.Actor, f.RecordFound = ActorAgent, false
		}, DeletionActorNotAllowed, nil},
		{"missing record", func(f *RecordDeletionFacts) { f.RecordFound = false }, DeletionNotFound, nil},
		{"foreign project", func(f *RecordDeletionFacts) { f.Record.ProjectID = "other" }, DeletionNotFound, nil},
		{"foreign project with stale version", func(f *RecordDeletionFacts) {
			f.Record.ProjectID, f.ExpectedVersion = "other", 1
		}, DeletionNotFound, nil},
		{"unknown actor", func(f *RecordDeletionFacts) { f.Actor = "reviewer_bot" }, DeletionInvalidFacts, nil},
		{"empty actor", func(f *RecordDeletionFacts) { f.Actor = "" }, DeletionInvalidFacts, nil},
		{"no project", func(f *RecordDeletionFacts) { f.RequestProjectID = "" }, DeletionInvalidFacts, nil},
		{"no record", func(f *RecordDeletionFacts) { f.RequestRecordID = "" }, DeletionInvalidFacts, nil},
		{"zero expected version", func(f *RecordDeletionFacts) { f.ExpectedVersion = 0 }, DeletionInvalidFacts, nil},
		{"other record read", func(f *RecordDeletionFacts) { f.Record.ID = "other" }, DeletionInvalidFacts, nil},
		{"zero record version", func(f *RecordDeletionFacts) { f.Record.Version = 0 }, DeletionInvalidFacts, nil},
		{"no versions", func(f *RecordDeletionFacts) { f.Versions = nil }, DeletionInvalidFacts, nil},
		{"version without id", func(f *RecordDeletionFacts) { f.Versions[0].VersionID = "" }, DeletionInvalidFacts, nil},
		{"version without hash", func(f *RecordDeletionFacts) { f.Versions[0].ContentHash = "" }, DeletionInvalidFacts, nil},
		{"repeated version", func(f *RecordDeletionFacts) { f.Versions[0].VersionID = "v2" }, DeletionInvalidFacts, nil},
		{"current version missing", func(f *RecordDeletionFacts) { f.Record.CurrentVersionID = "v3" }, DeletionInvalidFacts, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := deletionFacts()
			tc.mutate(&facts)
			got := DecideRecordDeletion(facts)
			if got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tc.want)
			}
			var pending []string
			for _, version := range got.Versions {
				pending = append(pending, version.VersionID)
			}
			if !slices.Equal(pending, tc.pending) {
				t.Fatalf("pending versions = %v, want %v", pending, tc.pending)
			}
		})
	}
}

func TestDecidePayloadRemoval(t *testing.T) {
	for _, tc := range []struct {
		name   string
		facts  PayloadRemovalFacts
		want   PayloadRemovalKind
		status PayloadStatus
		intent IntentEffect
	}{
		{"intent recorded, not dispatched", PayloadRemovalFacts{PayloadPresent, DispatchIntentRecorded}, PayloadRemove, PayloadRemovedBeforeDispatch, IntentInvalidate},
		{"delivery unknown", PayloadRemovalFacts{PayloadPresent, DispatchDeliveryUnknown}, PayloadRemove, PayloadRemovedDeliveryUnknown, IntentForbidRetry},
		{"delivered", PayloadRemovalFacts{PayloadPresent, DispatchDelivered}, PayloadRemove, PayloadRemovedAfterDelivery, IntentUnchanged},
		{"already removed", PayloadRemovalFacts{PayloadRemovedAfterDelivery, DispatchDelivered}, PayloadAlreadyRemoved, "", ""},
		{"removed before dispatch", PayloadRemovalFacts{PayloadRemovedBeforeDispatch, DispatchIntentRecorded}, PayloadAlreadyRemoved, "", ""},
		{"unknown status", PayloadRemovalFacts{"gone", DispatchDelivered}, PayloadInvalidFacts, "", ""},
		{"empty status", PayloadRemovalFacts{"", DispatchDelivered}, PayloadInvalidFacts, "", ""},
		{"unknown dispatch state", PayloadRemovalFacts{PayloadPresent, "failed"}, PayloadInvalidFacts, "", ""},
		{"removed payload with unknown dispatch state", PayloadRemovalFacts{PayloadRemovedAfterDelivery, ""}, PayloadInvalidFacts, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DecidePayloadRemoval(tc.facts)
			if got.Kind != tc.want || got.Status != tc.status || got.Intent != tc.intent || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

// Every removed status is reachable, and only present is not a removal
// result, so the schema's status list and the rule cannot drift apart.
func TestPayloadRemovalCoversEveryRemovedStatus(t *testing.T) {
	reached := map[PayloadStatus]bool{}
	for _, state := range DispatchStates() {
		reached[DecidePayloadRemoval(PayloadRemovalFacts{Status: PayloadPresent, Dispatch: state}).Status] = true
	}
	for _, status := range PayloadStatuses() {
		if reached[status] == (status == PayloadPresent) {
			t.Fatalf("status %s reached = %v", status, reached[status])
		}
	}
}

// The lists are what storage CHECKs mirror; they name every constant once.
func TestDeletionEnumerations(t *testing.T) {
	if got := DeletionSubjectKinds(); !slices.Equal(got, []DeletionSubjectKind{DeletionSubjectMemoryVersion, DeletionSubjectManifestPayload}) {
		t.Fatalf("DeletionSubjectKinds() = %v", got)
	}
	if got := PayloadStatuses(); !slices.Equal(got, []PayloadStatus{PayloadPresent, PayloadRemovedBeforeDispatch, PayloadRemovedDeliveryUnknown, PayloadRemovedAfterDelivery}) {
		t.Fatalf("PayloadStatuses() = %v", got)
	}
}
