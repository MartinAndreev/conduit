package rules

import "testing"

func TestSteerTechnicalRules(t *testing.T) {
	good := SteerEligibilityFacts{SessionMatches: true, ActiveTurn: true, TurnMatches: true}
	cases := []struct {
		name  string
		facts SteerEligibilityFacts
		want  SteerEligibilityKind
	}{
		{"allowed", good, SteerEligible},
		{"wrong session", SteerEligibilityFacts{ActiveTurn: true, TurnMatches: true}, SteerBindingStale},
		{"missing turn", SteerEligibilityFacts{SessionMatches: true, TurnMatches: true}, SteerBindingStale},
		{"wrong turn", SteerEligibilityFacts{SessionMatches: true, ActiveTurn: true}, SteerBindingStale},
		{"pending", SteerEligibilityFacts{SessionMatches: true, ActiveTurn: true, TurnMatches: true, SteerPending: true}, SteerBindingStale},
		{"uncertain", SteerEligibilityFacts{SessionMatches: true, ActiveTurn: true, TurnMatches: true, DeliveryUnknown: true}, SteerBindingStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideSteerEligibility(tc.facts)
			if d.Kind != tc.want || d.Reason == "" {
				t.Fatalf("decision=%#v", d)
			}
		})
	}
	if DecideSteerMode(true).Kind != SteerModeAllowed || DecideSteerMode(false).Kind != SteerModeUnsupported {
		t.Fatal("mode policy changed")
	}
	for _, f := range []SteerReceiptFacts{{true, true, true}, {false, true, true}, {true, false, true}, {true, true, false}} {
		d := DecideSteerReceipt(f)
		accepted := f.ActiveTurn && f.TurnMatches && f.RevisionMatches
		if (d.Kind == SteerReceiptAccepted) != accepted || d.Reason == "" {
			t.Fatalf("receipt=%#v", d)
		}
	}
}
