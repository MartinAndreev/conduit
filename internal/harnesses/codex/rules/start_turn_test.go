package rules

import "testing"

func TestDecideStartTurnPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		facts StartTurnFacts
		want  StartTurnKind
	}{
		{"closing precedes occupied and binding", StartTurnFacts{Closing: true, TurnPending: true}, StartTurnClosing},
		{"pending precedes binding", StartTurnFacts{TurnPending: true}, StartTurnOccupied},
		{"active precedes binding", StartTurnFacts{ActiveTurn: true}, StartTurnOccupied},
		{"session differs", StartTurnFacts{AssignmentIdentityMatches: true}, StartTurnBindingDiffers},
		{"assignment differs", StartTurnFacts{SessionMatches: true}, StartTurnBindingDiffers},
		{"allowed", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true}, StartTurnAllowed},
		{"closing precedes release", StartTurnFacts{Closing: true, ReleaseRequested: true}, StartTurnClosing},
		{"release with nothing unresolved", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, ReleaseRequested: true}, StartTurnReleaseNothingUnresolved},
		{"release mismatch precedes occupied", StartTurnFacts{ActiveTurn: true, ReleaseRequested: true, SteerUnresolved: true}, StartTurnReleaseMismatch},
		{"matching release still occupied", StartTurnFacts{ActiveTurn: true, ReleaseRequested: true, SteerUnresolved: true, ReleaseMatchesUnresolvedSteer: true}, StartTurnOccupied},
		{"matching release allowed", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, ReleaseRequested: true, SteerUnresolved: true, ReleaseMatchesUnresolvedSteer: true}, StartTurnAllowed},
		{"unresolved steer alone does not block", StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, SteerUnresolved: true}, StartTurnAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideStartTurn(tt.facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}
