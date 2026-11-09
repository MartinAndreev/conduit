package memory

import (
	"math"
	"slices"
	"testing"
	"time"
)

var (
	older = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer = older.Add(time.Hour)
)

func candidate(id string, tokens int64, trust Trust, at time.Time, relevance float64) MemoryCandidate {
	return MemoryCandidate{ID: id, Tokens: tokens, Trust: trust, RecencyAt: at, Relevance: relevance}
}

func manyCandidates(count int) []MemoryCandidate {
	candidates := make([]MemoryCandidate, 0, count)
	for i := range count {
		candidates = append(candidates, candidate(string(rune('a'+i)), 1_000_000, TrustCodeDerived, older, float64(count-i)))
	}
	return candidates
}

func TestDecideManifestAdmission(t *testing.T) {
	mandatory := func(tokens int64) []MandatoryItem { return []MandatoryItem{{ID: "m", Tokens: tokens}} }
	one := []MemoryCandidate{candidate("a", 10, TrustCodeDerived, older, 1)}
	tests := []struct {
		name         string
		facts        ManifestAdmissionFacts
		want         ManifestAdmissionKind
		wantBudget   int64
		wantAdmitted []string
		wantExcluded map[string]AdmissionExclusionKind
	}{
		{"no candidates", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: mandatory(50)}, AdmissionAdmitted, 10, nil, nil},
		{"budget is twenty percent of remainder", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, Mandatory: mandatory(200), HistoryTokens: 300}, AdmissionAdmitted, 100, nil, nil},
		{"budget rounds down", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 104}, AdmissionAdmitted, 20, nil, nil},
		{"native possible reserves ten percent of limit", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, NativeContextPossible: true}, AdmissionAdmitted, 180, nil, nil},
		{"reserve is taken from the limit not the remainder", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, Mandatory: mandatory(500), NativeContextPossible: true}, AdmissionAdmitted, 80, nil, nil},
		{"no reserve when native not possible", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000}, AdmissionAdmitted, 200, nil, nil},
		{"unknown limit has no reserve", ManifestAdmissionFacts{NativeContextPossible: true, Candidates: one}, AdmissionAdmitted, 0, []string{"a"}, nil},
		{"candidate exactly fills budget", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 50, Candidates: one}, AdmissionAdmitted, 10, []string{"a"}, nil},
		{"candidate one token over budget", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 49, Candidates: one}, AdmissionAdmitted, 9, nil, map[string]AdmissionExclusionKind{"a": ExclusionMemoryBudget}},
		{"mandatory exactly at limit leaves no memory", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: mandatory(100), Candidates: one}, AdmissionAdmitted, 0, nil, map[string]AdmissionExclusionKind{"a": ExclusionMemoryBudget}},
		{"trust orders before recency", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, Candidates: []MemoryCandidate{
			candidate("code", 10, TrustCodeDerived, newer, 9), candidate("human", 10, TrustHumanApproved, older, 1),
		}}, AdmissionAdmitted, 200, []string{"human", "code"}, nil},
		{"fixed trust order", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, Candidates: []MemoryCandidate{
			candidate("import", 10, TrustExternalImport, newer, 9), candidate("code", 10, TrustCodeDerived, newer, 9),
			candidate("observed", 10, TrustConduitObserved, newer, 9), candidate("human", 10, TrustHumanApproved, older, 1),
		}}, AdmissionAdmitted, 200, []string{"human", "observed", "code", "import"}, nil},
		{"recency orders before relevance", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, Candidates: []MemoryCandidate{
			candidate("old", 10, TrustCodeDerived, older, 9), candidate("new", 10, TrustCodeDerived, newer, 1),
		}}, AdmissionAdmitted, 200, []string{"new", "old"}, nil},
		{"relevance breaks ties", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, Candidates: []MemoryCandidate{
			candidate("low", 10, TrustCodeDerived, older, 1), candidate("high", 10, TrustCodeDerived, older, 9),
		}}, AdmissionAdmitted, 200, []string{"high", "low"}, nil},
		{"full tie keeps input order", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 1000, Candidates: []MemoryCandidate{
			candidate("first", 10, TrustCodeDerived, older, 1), candidate("second", 10, TrustCodeDerived, older, 1),
		}}, AdmissionAdmitted, 200, []string{"first", "second"}, nil},
		{"no later candidate fits", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Candidates: []MemoryCandidate{
			candidate("a", 15, TrustCodeDerived, newer, 1), candidate("b", 6, TrustCodeDerived, older, 1), candidate("c", 6, TrustCodeDerived, older, 0),
		}}, AdmissionAdmitted, 20, []string{"a"}, map[string]AdmissionExclusionKind{"b": ExclusionMemoryBudget, "c": ExclusionMemoryBudget}},
		{"unknown limit admits five", ManifestAdmissionFacts{Mandatory: mandatory(1 << 40), HistoryTokens: 1 << 40, Candidates: manyCandidates(5)}, AdmissionAdmitted, 0, []string{"a", "b", "c", "d", "e"}, nil},
		{"unknown limit excludes the sixth", ManifestAdmissionFacts{Candidates: manyCandidates(6)}, AdmissionAdmitted, 0, []string{"a", "b", "c", "d", "e"}, map[string]AdmissionExclusionKind{"f": ExclusionItemLimit}},

		{"mandatory over limit", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: mandatory(101), Candidates: one}, AdmissionBlockedMandatoryOverBudget, 0, nil, map[string]AdmissionExclusionKind{"a": ExclusionMandatoryBlocked}},
		{"summed mandatory over limit", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: []MandatoryItem{{ID: "x", Tokens: 60}, {ID: "y", Tokens: 41}}}, AdmissionBlockedMandatoryOverBudget, 0, nil, nil},

		{"history exactly exhausts remainder", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: mandatory(90), HistoryTokens: 10}, AdmissionAdmitted, 0, nil, nil},
		{"selection continues past a non-fitting candidate", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Candidates: []MemoryCandidate{
			candidate("big", 21, TrustCodeDerived, newer, 1), candidate("small", 5, TrustCodeDerived, older, 1),
		}}, AdmissionAdmitted, 20, []string{"small"}, map[string]AdmissionExclusionKind{"big": ExclusionMemoryBudget}},
		{"continued selection spends what is left", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Candidates: []MemoryCandidate{
			candidate("a", 15, TrustCodeDerived, newer, 3), candidate("b", 6, TrustCodeDerived, newer, 2), candidate("c", 5, TrustCodeDerived, newer, 1), candidate("d", 1, TrustCodeDerived, older, 1),
		}}, AdmissionAdmitted, 20, []string{"a", "c"}, map[string]AdmissionExclusionKind{"b": ExclusionMemoryBudget, "d": ExclusionMemoryBudget}},

		{"blocked by history", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: mandatory(90), HistoryTokens: 11, Candidates: one}, AdmissionBlockedMandatoryOverBudget, 0, nil, map[string]AdmissionExclusionKind{"a": ExclusionMandatoryBlocked}},
		{"blocked by native reserve", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: mandatory(91), NativeContextPossible: true, Candidates: one}, AdmissionBlockedMandatoryOverBudget, 0, nil, map[string]AdmissionExclusionKind{"a": ExclusionMandatoryBlocked}},
		{"reserve rounds up", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 105, NativeContextPossible: true}, AdmissionAdmitted, 18, nil, nil},
		{"rounded up reserve exactly exhausts remainder", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 101, Mandatory: mandatory(90), NativeContextPossible: true}, AdmissionAdmitted, 0, nil, nil},
		{"rounded up reserve blocks", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 101, Mandatory: mandatory(91), NativeContextPossible: true}, AdmissionBlockedMandatoryOverBudget, 0, nil, nil},
		{"mandatory sum would overflow", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: math.MaxInt64, Mandatory: []MandatoryItem{{ID: "x", Tokens: math.MaxInt64}, {ID: "y", Tokens: math.MaxInt64}}, Candidates: one}, AdmissionBlockedMandatoryOverBudget, 0, nil, map[string]AdmissionExclusionKind{"a": ExclusionMandatoryBlocked}},
		{"huge history after mandatory", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: math.MaxInt64, Mandatory: mandatory(1), HistoryTokens: math.MaxInt64}, AdmissionBlockedMandatoryOverBudget, 0, nil, nil},
		{"history one below the limit", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: math.MaxInt64, HistoryTokens: math.MaxInt64 - 1}, AdmissionAdmitted, 0, nil, nil},
		{"budget percentage at max limit", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: math.MaxInt64}, AdmissionAdmitted, 1844674407370955161, nil, nil},
		{"reserve percentage at max limit", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: math.MaxInt64, NativeContextPossible: true}, AdmissionAdmitted, 1660206966633859645, nil, nil},
		{"unknown limit ignores huge totals", ManifestAdmissionFacts{Mandatory: []MandatoryItem{{ID: "x", Tokens: math.MaxInt64}, {ID: "y", Tokens: math.MaxInt64}}, HistoryTokens: math.MaxInt64, Candidates: one}, AdmissionAdmitted, 0, []string{"a"}, nil},
		{"reserve exactly exhausts remainder", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, Mandatory: mandatory(90), NativeContextPossible: true}, AdmissionAdmitted, 0, nil, nil},

		{"known limit of zero", ManifestAdmissionFacts{ModelLimitKnown: true}, AdmissionInvalidFacts, 0, nil, nil},
		{"negative history", ManifestAdmissionFacts{ModelLimitKnown: true, ModelLimit: 100, HistoryTokens: -1}, AdmissionInvalidFacts, 0, nil, nil},
		{"mandatory without id", ManifestAdmissionFacts{Mandatory: []MandatoryItem{{Tokens: 1}}}, AdmissionInvalidFacts, 0, nil, nil},
		{"negative mandatory", ManifestAdmissionFacts{Mandatory: []MandatoryItem{{ID: "m", Tokens: -1}}}, AdmissionInvalidFacts, 0, nil, nil},
		{"candidate without id", ManifestAdmissionFacts{Candidates: []MemoryCandidate{candidate("", 1, TrustCodeDerived, older, 1)}}, AdmissionInvalidFacts, 0, nil, nil},
		{"negative candidate", ManifestAdmissionFacts{Candidates: []MemoryCandidate{candidate("a", -1, TrustCodeDerived, older, 1)}}, AdmissionInvalidFacts, 0, nil, nil},
		{"agent claim cannot be a candidate", ManifestAdmissionFacts{Candidates: []MemoryCandidate{candidate("a", 1, TrustAgentClaim, older, 1)}}, AdmissionInvalidFacts, 0, nil, nil},
		{"candidate with unknown trust", ManifestAdmissionFacts{Candidates: []MemoryCandidate{candidate("a", 1, "score", older, 1)}}, AdmissionInvalidFacts, 0, nil, nil},
		{"candidate without recency", ManifestAdmissionFacts{Candidates: []MemoryCandidate{candidate("a", 1, TrustCodeDerived, time.Time{}, 1)}}, AdmissionInvalidFacts, 0, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideManifestAdmission(tt.facts)
			if got.Kind != tt.want || got.Reason == "" || got.MemoryBudget != tt.wantBudget || !slices.Equal(got.Admitted, tt.wantAdmitted) {
				t.Fatalf("decision = %+v, want %s budget %d admitted %v", got, tt.want, tt.wantBudget, tt.wantAdmitted)
			}
			if len(got.Excluded) != len(tt.wantExcluded) {
				t.Fatalf("excluded = %+v, want %v", got.Excluded, tt.wantExcluded)
			}
			for _, exclusion := range got.Excluded {
				if tt.wantExcluded[exclusion.CandidateID] != exclusion.Kind || exclusion.Reason == "" {
					t.Fatalf("exclusion = %+v, want %v", exclusion, tt.wantExcluded)
				}
			}
		})
	}
}

func TestDecideManifestAdmissionLeavesInputOrder(t *testing.T) {
	candidates := []MemoryCandidate{candidate("low", 1, TrustCodeDerived, older, 1), candidate("high", 1, TrustCodeDerived, newer, 1)}
	DecideManifestAdmission(ManifestAdmissionFacts{Candidates: candidates})
	if candidates[0].ID != "low" {
		t.Fatalf("candidates were reordered: %+v", candidates)
	}
}
