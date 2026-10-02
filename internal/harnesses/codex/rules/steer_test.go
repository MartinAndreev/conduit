package rules

import "testing"

func TestSteerEligibilityPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		facts SteerFacts
		kind  SteerKind
	}{
		{"session before mode and binding", SteerFacts{}, SteerSessionDiffers},
		{"mode before binding", SteerFacts{SessionMatches: true}, SteerModeUnsupported},
		{"missing turn", SteerFacts{SessionMatches: true, ActiveTurnMode: true}, SteerBindingUnresolved},
		{"pending correction", SteerFacts{SessionMatches: true, ActiveTurnMode: true, ActiveTurnMatches: true, SteerPending: true}, SteerBindingUnresolved},
		{"pending start", SteerFacts{SessionMatches: true, ActiveTurnMode: true, ActiveTurnMatches: true, TurnPending: true}, SteerBindingUnresolved},
		{"allowed", SteerFacts{SessionMatches: true, ActiveTurnMode: true, ActiveTurnMatches: true}, SteerAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideSteer(tc.facts)
			if d.Kind != tc.kind || d.Reason == "" {
				t.Fatalf("decision = %#v", d)
			}
		})
	}
}

func TestSteerReceiptRequiresEveryCorrelationFact(t *testing.T) {
	for _, facts := range []SteerReceiptFacts{{false, true, true}, {true, false, true}, {true, true, false}, {true, true, true}} {
		d := DecideSteerReceipt(facts)
		want := SteerReceiptBindingDiffers
		if facts.PendingRequestMatches && facts.ActiveTurnMatches && facts.NativeTurnMatches {
			want = SteerReceiptAccepted
		}
		if d.Kind != want {
			t.Fatalf("facts %#v decision %#v", facts, d)
		}
	}
}
