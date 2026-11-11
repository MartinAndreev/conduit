package redaction

import (
	"cmp"
	"slices"
)

// MaterialFacts describe one redacted piece of context material.
type MaterialFacts struct {
	// Mandatory is true for material that must reach the agent in full.
	Mandatory bool
	Findings  []Finding
}

type MaterialKind string

const (
	MaterialClean                  MaterialKind = "clean"
	MaterialRedacted               MaterialKind = "redacted"
	MaterialBlockedUnsafeMandatory MaterialKind = "blocked_unsafe_mandatory"
	MaterialInvalidFacts           MaterialKind = "invalid_facts"
)

type MaterialDecision struct {
	Kind   MaterialKind
	Reason string
}

// DecideRedactedMaterial answers whether redacted material may reach the agent.
// Mandatory material cannot be masked without losing what made it mandatory,
// so any finding blocks it; optional material is included with masked values.
func DecideRedactedMaterial(f MaterialFacts) MaterialDecision {
	if !validFindings(f.Findings) {
		return MaterialDecision{Kind: MaterialInvalidFacts, Reason: "findings need a known kind, a positive count, and one entry per kind"}
	}
	switch {
	case len(f.Findings) == 0:
		return MaterialDecision{Kind: MaterialClean, Reason: "no secret was detected"}
	case f.Mandatory:
		return MaterialDecision{Kind: MaterialBlockedUnsafeMandatory, Reason: "mandatory material contains detected secrets"}
	default:
		return MaterialDecision{Kind: MaterialRedacted, Reason: "optional material is included with detected secrets masked"}
	}
}

func validFindings(findings []Finding) bool {
	seen := make(map[Kind]bool, len(findings))
	for _, f := range findings {
		if f.Count <= 0 || seen[f.Kind] || !knownKind(f.Kind) {
			return false
		}
		seen[f.Kind] = true
	}
	return true
}

// knownKind derives membership from the rule table, the single owner of kinds.
func knownKind(kind Kind) bool {
	return slices.ContainsFunc(rules, func(r rule) bool { return r.kind == kind })
}

func sortFindings(findings []Finding) {
	slices.SortFunc(findings, func(a, b Finding) int { return cmp.Compare(a.Kind, b.Kind) })
}
