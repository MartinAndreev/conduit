package rules

import "testing"

func TestCloseEligibility(t *testing.T) {

	cases := []struct {
		name  string
		facts CloseFacts
		want  CloseKind
	}{
		{
			name:  "stale precedes active",
			facts: CloseFacts{ActiveTurn: true},
			want:  CloseBindingStale,
		},
		{
			name:  "active requires detach",
			facts: CloseFacts{SessionMatches: true, ActiveTurn: true},
			want:  CloseActiveWorkRequiresDetach,
		},
		{
			name:  "detach",
			facts: CloseFacts{SessionMatches: true, ActiveTurn: true, ExplicitDetach: true},
			want:  CloseAllowed,
		},
		{
			name:  "idle",
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
