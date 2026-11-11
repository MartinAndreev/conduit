package memory

import "testing"

func TestDecideManifestRecheck(t *testing.T) {
	expected := []ExpectedRecordVersion{
		{RecordID: "r1", VersionID: "v1", RecordVersion: 3},
		{RecordID: "r2", VersionID: "v2", RecordVersion: 1},
	}
	unchanged := func() []CurrentRecordState {
		return []CurrentRecordState{
			{RecordID: "r2", Status: StatusNeedsVerification, CurrentVersionID: "v2", RecordVersion: 1},
			{RecordID: "r1", Status: StatusActive, CurrentVersionID: "v1", RecordVersion: 3},
		}
	}
	with := func(change func([]CurrentRecordState) []CurrentRecordState) []CurrentRecordState {
		return change(unchanged())
	}
	tests := []struct {
		name       string
		expected   []ExpectedRecordVersion
		current    []CurrentRecordState
		want       ManifestRecheckKind
		wantRecord string
		wantChange RecheckChange
	}{
		{"nothing expected", nil, nil, RecheckCommit, "", ""},
		{"unchanged", expected, unchanged(), RecheckCommit, "", ""},
		{"unrelated current records are ignored", expected[:1], unchanged(), RecheckCommit, "", ""},
		{"deleted", expected, unchanged()[:1], RecheckRecompose, "r1", ChangeRecordDeleted},
		{"superseded", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[1].Status, c[1].CurrentVersionID, c[1].RecordVersion = StatusSuperseded, "v9", 4
			return c
		}), RecheckRecompose, "r1", ChangeSuperseded},
		{"archived", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[1].Status = StatusArchived
			return c
		}), RecheckRecompose, "r1", ChangeLifecycleChanged},
		{"new current version", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].CurrentVersionID = "v3"
			return c
		}), RecheckRecompose, "r2", ChangeVersionChanged},
		{"content deleted with unchanged versions", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].ContentDeleted = true
			return c
		}), RecheckRecompose, "r2", ChangeContentDeleted},
		{"content deleted and version bumped", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].ContentDeleted, c[0].RecordVersion = true, 2
			return c
		}), RecheckRecompose, "r2", ChangeContentDeleted},
		{"new version precedes deleted content", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].ContentDeleted, c[0].CurrentVersionID = true, "v3"
			return c
		}), RecheckRecompose, "r2", ChangeVersionChanged},
		{"record version bumped", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].RecordVersion = 2
			return c
		}), RecheckRecompose, "r2", ChangeRecordChanged},
		{"first changed record in expected order", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].RecordVersion, c[1].RecordVersion = 2, 4
			return c
		}), RecheckRecompose, "r1", ChangeRecordChanged},

		{"duplicate expected", append(expected, expected[0]), unchanged(), RecheckInvalidFacts, "", ""},
		{"expected without version", []ExpectedRecordVersion{{RecordID: "r1", RecordVersion: 1}}, unchanged(), RecheckInvalidFacts, "", ""},
		{"expected without record", []ExpectedRecordVersion{{VersionID: "v1", RecordVersion: 1}}, unchanged(), RecheckInvalidFacts, "", ""},
		{"expected zero record version", []ExpectedRecordVersion{{RecordID: "r1", VersionID: "v1"}}, unchanged(), RecheckInvalidFacts, "", ""},
		{"duplicate current", expected, append(unchanged(), unchanged()[0]), RecheckInvalidFacts, "", ""},
		{"current unknown status", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].Status = "gone"
			return c
		}), RecheckInvalidFacts, "", ""},
		{"current without version id", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].CurrentVersionID = ""
			return c
		}), RecheckInvalidFacts, "", ""},
		{"current zero record version", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].RecordVersion = 0
			return c
		}), RecheckInvalidFacts, "", ""},
		{"current without record id", expected, with(func(c []CurrentRecordState) []CurrentRecordState {
			c[0].RecordID = ""
			return c
		}), RecheckInvalidFacts, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideManifestRecheck(ManifestRecheckFacts{Expected: tt.expected, Current: tt.current})
			if got.Kind != tt.want || got.Reason == "" || got.RecordID != tt.wantRecord || got.Change != tt.wantChange {
				t.Fatalf("decision = %+v, want kind %q record %q change %q", got, tt.want, tt.wantRecord, tt.wantChange)
			}
		})
	}
}
