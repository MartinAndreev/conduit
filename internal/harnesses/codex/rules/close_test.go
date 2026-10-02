package rules

import "testing"

func TestOwnedProcessEvidenceRequiresAllProof(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		facts := QuiescenceFacts{RootReaped: mask&1 != 0, TreeKillSupported: mask&2 != 0, KillAttempted: mask&4 != 0, KillConfirmed: mask&8 != 0}
		want := QuiescenceUnproven
		if mask == 15 {
			want = QuiescenceConfirmed
		}
		if d := DecideQuiescence(facts); d.Kind != want {
			t.Fatalf("facts %#v decision %#v", facts, d)
		}
	}
	for _, tc := range []struct {
		reaped, supported bool
		kind              OwnedProcessTerminationKind
	}{{false, false, OwnedProcessKillRequired}, {false, true, OwnedProcessKillRequired}, {true, true, OwnedProcessKillRequired}, {true, false, OwnedProcessRootAlreadyReaped}} {
		if d := DecideOwnedProcessTermination(tc.reaped, tc.supported); d.Kind != tc.kind {
			t.Fatalf("decision %#v", d)
		}
	}
}
func TestCloseEligibility(t *testing.T) {

	cases := []struct {
		name  string
		facts CloseFacts
		want  CloseKind
	}{
		{
			name:  "foreign session precedes closing",
			facts: CloseFacts{Closing: true},
			want:  CloseSessionDiffers,
		},
		{
			name:  "closing precedes unresolved work",
			facts: CloseFacts{SessionMatches: true, Closing: true, RequireQuiescent: true, ActiveOrUnresolvedWork: true},
			want:  CloseAlreadyClosing,
		},
		{
			name:  "unresolved work precedes detach",
			facts: CloseFacts{SessionMatches: true, RequireQuiescent: true, ActiveOrUnresolvedWork: true, ExplicitDetach: true},
			want:  CloseWorkUnresolved,
		},
		{
			name:  "owned process cannot detach",
			facts: CloseFacts{SessionMatches: true, ExplicitDetach: true},
			want:  CloseDetachUnsupported,
		},
		{
			name:  "allowed",
			facts: CloseFacts{SessionMatches: true},
			want:  CloseAllowed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideClose(tc.facts)
			if decision.Kind != tc.want || decision.Reason == "" {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}
