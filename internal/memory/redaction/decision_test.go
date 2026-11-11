package redaction

import (
	"slices"
	"testing"
)

func TestDecideRedactedMaterial(t *testing.T) {
	one := []Finding{{Kind: KindJWT, Count: 1}}
	tests := []struct {
		name  string
		facts MaterialFacts
		want  MaterialKind
	}{
		{"optional clean", MaterialFacts{}, MaterialClean},
		{"mandatory clean", MaterialFacts{Mandatory: true}, MaterialClean},
		{"optional with findings is masked", MaterialFacts{Findings: one}, MaterialRedacted},
		{"mandatory with findings is blocked", MaterialFacts{Mandatory: true, Findings: one}, MaterialBlockedUnsafeMandatory},
		{"several kinds", MaterialFacts{Findings: []Finding{{KindJWT, 2}, {KindURLPassword, 1}}}, MaterialRedacted},
		{"zero count", MaterialFacts{Findings: []Finding{{KindJWT, 0}}}, MaterialInvalidFacts},
		{"negative count", MaterialFacts{Mandatory: true, Findings: []Finding{{KindJWT, -1}}}, MaterialInvalidFacts},
		{"empty kind", MaterialFacts{Findings: []Finding{{"", 1}}}, MaterialInvalidFacts},
		{"unknown kind", MaterialFacts{Findings: []Finding{{"email", 1}}}, MaterialInvalidFacts},
		{"duplicate kind", MaterialFacts{Findings: []Finding{{KindJWT, 1}, {KindJWT, 1}}}, MaterialInvalidFacts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideRedactedMaterial(tt.facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want kind %q with a reason", got, tt.want)
			}
		})
	}
}

// The rule must accept exactly what the policy produces.
func TestRedactFindingsAreValidFacts(t *testing.T) {
	got := Policy{}.Redact("password=" + fakeSecret + " " + fakeAWS + " " + fakeAWS)
	if d := DecideRedactedMaterial(MaterialFacts{Mandatory: true, Findings: got.Findings}); d.Kind != MaterialBlockedUnsafeMandatory {
		t.Fatalf("decision = %+v, want blocked", d)
	}
}

func TestMergeFindings(t *testing.T) {
	got := MergeFindings([]Finding{{KindURLPassword, 1}, {KindJWT, 2}}, nil, []Finding{{KindJWT, 1}, {KindAWSAccessKeyID, 3}})
	want := []Finding{{KindAWSAccessKeyID, 3}, {KindJWT, 3}, {KindURLPassword, 1}}
	if !slices.Equal(got, want) {
		t.Fatalf("MergeFindings = %v, want %v", got, want)
	}
	if got := MergeFindings(nil, nil); got != nil {
		t.Fatalf("MergeFindings of nothing = %v, want nil", got)
	}
}
