package memory

// ExpectedRecordVersion is what a composition relied on for one included
// record: its current version and the record's optimistic version.
type ExpectedRecordVersion struct {
	RecordID      string
	VersionID     string
	RecordVersion int64
}

// CurrentRecordState is a record as read in the manifest's commit
// transaction. A record absent from the facts was deleted.
type CurrentRecordState struct {
	RecordID         string
	Status           RecordStatus
	CurrentVersionID string
	RecordVersion    int64
	// ContentDeleted is true when the current version's content was deleted.
	ContentDeleted bool
}

type ManifestRecheckFacts struct {
	Expected []ExpectedRecordVersion
	Current  []CurrentRecordState
}

type ManifestRecheckKind string

const (
	RecheckCommit       ManifestRecheckKind = "commit"
	RecheckRecompose    ManifestRecheckKind = "recompose"
	RecheckInvalidFacts ManifestRecheckKind = "invalid_facts"
)

// RecheckChange names what changed in the first record that forces a
// recomposition.
type RecheckChange string

const (
	ChangeRecordDeleted    RecheckChange = "record_deleted"
	ChangeSuperseded       RecheckChange = "superseded"
	ChangeLifecycleChanged RecheckChange = "lifecycle_changed"
	ChangeVersionChanged   RecheckChange = "version_changed"
	ChangeContentDeleted   RecheckChange = "content_deleted"
	ChangeRecordChanged    RecheckChange = "record_changed"
)

// ManifestRecheckDecision names the first changed record, in expected order,
// only when Kind is RecheckRecompose.
type ManifestRecheckDecision struct {
	Kind     ManifestRecheckKind
	Reason   string
	RecordID string
	Change   RecheckChange
}

// DecideManifestRecheck compares the versions a composition relied on with
// the ones read in the commit transaction. Any difference, including one
// that only bumps the record's optimistic version (a label may change),
// recomposes rather than commit a manifest built from other facts. Per
// record, deletion precedes supersession, then other lifecycle exits, then a
// new current version, then deleted content, then any other record change.
// Deleted content is checked on its own: deletion should also bump the
// record's version, but the manifest must not carry deleted text even if it
// did not.
func DecideManifestRecheck(f ManifestRecheckFacts) ManifestRecheckDecision {
	current, valid := currentByRecord(f.Current)
	if !valid || !validExpected(f.Expected) {
		return ManifestRecheckDecision{Kind: RecheckInvalidFacts, Reason: "expected and current records need ids, positive versions, a known status, and one entry per record"}
	}
	for _, want := range f.Expected {
		got, found := current[want.RecordID]
		var change RecheckChange
		var reason string
		switch {
		case !found:
			change, reason = ChangeRecordDeleted, "record was deleted after composition"
		case got.Status == StatusSuperseded:
			change, reason = ChangeSuperseded, "record was superseded after composition"
		case !retrievableStatus(got.Status):
			change, reason = ChangeLifecycleChanged, "record left a retrievable lifecycle after composition"
		case got.CurrentVersionID != want.VersionID:
			change, reason = ChangeVersionChanged, "record has another current version"
		case got.ContentDeleted:
			change, reason = ChangeContentDeleted, "record content was deleted after composition"
		case got.RecordVersion != want.RecordVersion:
			change, reason = ChangeRecordChanged, "record changed after composition"
		default:
			continue
		}
		return ManifestRecheckDecision{Kind: RecheckRecompose, Reason: reason, RecordID: want.RecordID, Change: change}
	}
	return ManifestRecheckDecision{Kind: RecheckCommit, Reason: "every included record is unchanged"}
}

func currentByRecord(states []CurrentRecordState) (map[string]CurrentRecordState, bool) {
	byID := make(map[string]CurrentRecordState, len(states))
	for _, s := range states {
		if _, seen := byID[s.RecordID]; seen || s.RecordID == "" || s.CurrentVersionID == "" || s.RecordVersion < 1 || !validStatus(s.Status) {
			return nil, false
		}
		byID[s.RecordID] = s
	}
	return byID, true
}

func validExpected(expected []ExpectedRecordVersion) bool {
	seen := make(map[string]bool, len(expected))
	for _, e := range expected {
		if seen[e.RecordID] || e.RecordID == "" || e.VersionID == "" || e.RecordVersion < 1 {
			return false
		}
		seen[e.RecordID] = true
	}
	return true
}
