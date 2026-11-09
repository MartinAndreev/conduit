package memory

import (
	"cmp"
	"slices"
	"time"
)

const (
	// memoryBudgetPercent is memory's maximum share of the remaining budget.
	memoryBudgetPercent = 20
	// nativeReservePercent is the share of the model limit, rounded up, kept
	// for native context in state possible, whose size is unknown.
	nativeReservePercent = 10
	// unknownLimitMemoryItems caps memory when the model limit is unknown.
	unknownLimitMemoryItems = 5
)

// admissionTrustOrder ranks trust classes from most to least preferred.
// agent_claim is absent because it cannot pass retrieval eligibility.
var admissionTrustOrder = []Trust{TrustHumanApproved, TrustConduitObserved, TrustCodeDerived, TrustExternalImport}

// MandatoryItem is material that must reach the agent in full.
type MandatoryItem struct {
	ID     string
	Tokens int64
}

// MemoryCandidate is one retrieval-eligible item competing for the budget.
type MemoryCandidate struct {
	ID        string
	Tokens    int64
	Trust     Trust
	RecencyAt time.Time
	Relevance float64
}

// ManifestAdmissionFacts carry the budget inputs.
type ManifestAdmissionFacts struct {
	Mandatory       []MandatoryItem
	Candidates      []MemoryCandidate
	ModelLimitKnown bool
	ModelLimit      int64
	// HistoryTokens is the session's accumulated history.
	HistoryTokens int64
	// NativeContextPossible is true when any native source is in state possible.
	NativeContextPossible bool
}

type ManifestAdmissionKind string

const (
	AdmissionAdmitted                   ManifestAdmissionKind = "admitted"
	AdmissionInvalidFacts               ManifestAdmissionKind = "invalid_facts"
	AdmissionBlockedMandatoryOverBudget ManifestAdmissionKind = "blocked_mandatory_over_budget"
)

type AdmissionExclusionKind string

const (
	ExclusionMandatoryBlocked AdmissionExclusionKind = "mandatory_blocked"
	ExclusionItemLimit        AdmissionExclusionKind = "item_limit"
	ExclusionMemoryBudget     AdmissionExclusionKind = "memory_budget"
)

// AdmissionExclusion records why a candidate stayed out of the manifest.
type AdmissionExclusion struct {
	CandidateID string
	Kind        AdmissionExclusionKind
	Reason      string
}

// ManifestAdmissionDecision lists admitted candidates in admission order.
// MemoryBudget is meaningful only when the model limit is known.
type ManifestAdmissionDecision struct {
	Kind         ManifestAdmissionKind
	Reason       string
	MemoryBudget int64
	Admitted     []string
	Excluded     []AdmissionExclusion
}

// DecideManifestAdmission reserves mandatory material first and never truncates
// it, then admits memory by trust, recency and relevance within memory's share.
func DecideManifestAdmission(f ManifestAdmissionFacts) ManifestAdmissionDecision {
	if !validAdmissionFacts(f) {
		return ManifestAdmissionDecision{Kind: AdmissionInvalidFacts, Reason: "budget, item, or candidate facts are invalid"}
	}
	ordered := orderCandidates(f.Candidates)
	if !f.ModelLimitKnown {
		return admitByCount(ordered)
	}
	remaining, fits := remainingBudget(f)
	if !fits {
		return blockedAdmission(f.Candidates)
	}
	return admitByBudget(ordered, percentDown(remaining, memoryBudgetPercent))
}

// remainingBudget spends the known limit item by item, so no valid input can
// overflow. It reports false when mandatory, history or reserve do not fit.
func remainingBudget(f ManifestAdmissionFacts) (int64, bool) {
	remaining := f.ModelLimit
	for _, item := range f.Mandatory {
		if item.Tokens > remaining {
			return 0, false
		}
		remaining -= item.Tokens
	}
	if f.HistoryTokens > remaining {
		return 0, false
	}
	remaining -= f.HistoryTokens
	if !f.NativeContextPossible {
		return remaining, true
	}
	reserve := percentUp(f.ModelLimit, nativeReservePercent)
	if reserve > remaining {
		return 0, false
	}
	return remaining - reserve, true
}

// percentDown and percentUp split value so the multiplication cannot overflow.
func percentDown(value, percent int64) int64 {
	return value/100*percent + value%100*percent/100
}

func percentUp(value, percent int64) int64 {
	return value/100*percent + (value%100*percent+99)/100
}

func validAdmissionFacts(f ManifestAdmissionFacts) bool {
	if (f.ModelLimitKnown && f.ModelLimit <= 0) || f.HistoryTokens < 0 {
		return false
	}
	for _, item := range f.Mandatory {
		if item.ID == "" || item.Tokens < 0 {
			return false
		}
	}
	for _, candidate := range f.Candidates {
		if candidate.ID == "" || candidate.Tokens < 0 || !slices.Contains(admissionTrustOrder, candidate.Trust) || candidate.RecencyAt.IsZero() {
			return false
		}
	}
	return true
}

func blockedAdmission(candidates []MemoryCandidate) ManifestAdmissionDecision {
	decision := ManifestAdmissionDecision{Kind: AdmissionBlockedMandatoryOverBudget, Reason: "mandatory material does not fit the model limit after history and reserve"}
	for _, candidate := range candidates {
		decision.Excluded = append(decision.Excluded, AdmissionExclusion{
			CandidateID: candidate.ID,
			Kind:        ExclusionMandatoryBlocked,
			Reason:      "mandatory material is over budget",
		})
	}
	return decision
}

// orderCandidates sorts a copy; equal candidates keep their input order.
func orderCandidates(candidates []MemoryCandidate) []MemoryCandidate {
	ordered := slices.Clone(candidates)
	slices.SortStableFunc(ordered, func(a, b MemoryCandidate) int {
		return cmp.Or(
			cmp.Compare(slices.Index(admissionTrustOrder, a.Trust), slices.Index(admissionTrustOrder, b.Trust)),
			b.RecencyAt.Compare(a.RecencyAt),
			cmp.Compare(b.Relevance, a.Relevance),
		)
	})
	return ordered
}

func admitByCount(ordered []MemoryCandidate) ManifestAdmissionDecision {
	decision := ManifestAdmissionDecision{Kind: AdmissionAdmitted, Reason: "model limit unknown; memory limited by item count"}
	for i, candidate := range ordered {
		if i < unknownLimitMemoryItems {
			decision.Admitted = append(decision.Admitted, candidate.ID)
			continue
		}
		decision.Excluded = append(decision.Excluded, AdmissionExclusion{
			CandidateID: candidate.ID,
			Kind:        ExclusionItemLimit,
			Reason:      "memory item limit reached for an unknown model limit",
		})
	}
	return decision
}

// admitByBudget excludes a candidate that does not fit and keeps filling with
// lower-ranked ones.
func admitByBudget(ordered []MemoryCandidate, budget int64) ManifestAdmissionDecision {
	decision := ManifestAdmissionDecision{Kind: AdmissionAdmitted, Reason: "memory admitted within its budget share", MemoryBudget: budget}
	left := budget
	for _, candidate := range ordered {
		if candidate.Tokens <= left {
			left -= candidate.Tokens
			decision.Admitted = append(decision.Admitted, candidate.ID)
			continue
		}
		decision.Excluded = append(decision.Excluded, AdmissionExclusion{
			CandidateID: candidate.ID,
			Kind:        ExclusionMemoryBudget,
			Reason:      "candidate does not fit the remaining memory budget",
		})
	}
	return decision
}
