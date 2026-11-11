package memory

import (
	"slices"
	"testing"
)

// The lists are what storage CHECKs mirror; they name every constant once.
func TestReviewEnumerations(t *testing.T) {
	if got := Actors(); !slices.Equal(got, []Actor{ActorHuman, ActorSystem, ActorAgent}) {
		t.Fatalf("Actors() = %v", got)
	}
	if got := ReviewDecisions(); !slices.Equal(got, []ReviewDecision{ReviewApprove, ReviewReject, ReviewOpinion}) {
		t.Fatalf("ReviewDecisions() = %v", got)
	}
}
