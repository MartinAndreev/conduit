package rules

import "testing"

func TestCloseEligibility(t *testing.T) {
	cases := []struct {
		name        string
		facts       CloseFacts
		want        CloseKind
		stopsClient bool
		closes      bool
	}{
		{"stale precedes active", CloseFacts{ActiveTurn: true, InterruptThenClose: true}, CloseBindingStale, false, false},
		{"active requires detach", CloseFacts{SessionMatches: true, ActiveTurn: true}, CloseActiveWorkRequiresDetach, false, false},
		{"active interrupt", CloseFacts{SessionMatches: true, ActiveTurn: true, InterruptThenClose: true}, CloseStopClientAndServer, true, true},
		{"active detach", CloseFacts{SessionMatches: true, ActiveTurn: true, ExplicitDetach: true}, CloseDetachStopsServer, true, true},
		{"idle", CloseFacts{SessionMatches: true}, CloseStopServer, false, true},
		{"idle interrupt", CloseFacts{SessionMatches: true, InterruptThenClose: true}, CloseStopServer, false, true},
		{"idle detach", CloseFacts{SessionMatches: true, ExplicitDetach: true}, CloseStopServer, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideClose(tc.facts)
			if decision.Kind != tc.want || decision.Reason == "" || decision.StopsClient() != tc.stopsClient || decision.Closes() != tc.closes {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}
