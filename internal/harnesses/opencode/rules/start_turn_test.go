package rules

import "testing"

func TestDecideStartTurnPrecedence(t *testing.T) {
	allowed := StartTurnFacts{SessionMatches: true, AssignmentIdentityMatches: true, AssignmentRevisionMatches: true, ResultContractMatches: true}
	tests := []struct {
		name   string
		change func(*StartTurnFacts)
		want   StartTurnKind
	}{
		{"allowed", func(f *StartTurnFacts) {}, StartTurnAllowed},
		{"session precedes unavailable", func(f *StartTurnFacts) { f.SessionMatches = false; f.Closed = true; f.ResultContractMatches = false }, StartTurnBindingDiffers},
		{"identity differs", func(f *StartTurnFacts) { f.AssignmentIdentityMatches = false }, StartTurnBindingDiffers},
		{"revision precedes unavailable", func(f *StartTurnFacts) { f.AssignmentRevisionMatches = false; f.ActiveTurn = true }, StartTurnBindingDiffers},
		{"closed precedes contract", func(f *StartTurnFacts) { f.Closed = true; f.ResultContractMatches = false }, StartTurnUnavailable},
		{"active", func(f *StartTurnFacts) { f.ActiveTurn = true }, StartTurnUnavailable},
		{"steer pending", func(f *StartTurnFacts) { f.SteerPending = true }, StartTurnUnavailable},
		{"unknown delivery", func(f *StartTurnFacts) { f.DeliveryUnknown = true }, StartTurnUnavailable},
		{"lost", func(f *StartTurnFacts) { f.SessionLost = true }, StartTurnUnavailable},
		{"contract differs", func(f *StartTurnFacts) { f.ResultContractMatches = false }, StartTurnContractChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts := allowed
			tt.change(&facts)
			got := DecideStartTurn(facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}
