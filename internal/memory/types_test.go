package memory

import (
	"slices"
	"strings"
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

func TestRecordEnumerations(t *testing.T) {
	if got := Categories(); !slices.Equal(got, []RecordCategory{CategoryDecision, CategoryConvention, CategoryConstraint, CategoryFact, CategoryGotcha}) {
		t.Fatalf("Categories() = %v", got)
	}
	if got := RelationKinds(); !slices.Equal(got, []RelationKind{RelationSupersedes, RelationContradicts}) {
		t.Fatalf("RelationKinds() = %v", got)
	}
	if got := Producers(); !slices.Equal(got, []Producer{ProducerAgentProposal, ProducerConduitObservation, ProducerHuman}) {
		t.Fatalf("Producers() = %v", got)
	}
	for _, producer := range Producers() {
		if _, ok := producerRules[producer]; !ok {
			t.Fatalf("producer %s has no rule", producer)
		}
	}
	if len(producerRules) != len(Producers()) {
		t.Fatalf("producer rules = %d", len(producerRules))
	}
}

// The hash is of the exact bytes: "sha256:" and 64 lowercase hex digits.
func TestContentHash(t *testing.T) {
	if got := ContentHash(""); got != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("ContentHash(\"\") = %s", got)
	}
	got := ContentHash("Решение")
	if !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 || strings.ToLower(got) != got || got == ContentHash("решение") {
		t.Fatalf("ContentHash = %s", got)
	}
}
