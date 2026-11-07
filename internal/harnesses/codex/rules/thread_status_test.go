package rules

import "testing"

func TestDecideThreadStatusTransition(t *testing.T) {
	tests := []struct {
		name  string
		facts ThreadStatusFacts
		want  ThreadStatusKind
	}{
		{"open session applies", ThreadStatusFacts{}, ThreadStatusApply},
		{"closing ignores", ThreadStatusFacts{Closing: true}, ThreadStatusIgnoredWhileEnded},
		{"lost or closed ignores", ThreadStatusFacts{Ended: true}, ThreadStatusIgnoredWhileEnded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideThreadStatusTransition(tt.facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}
