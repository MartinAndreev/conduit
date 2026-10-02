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
