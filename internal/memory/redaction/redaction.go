// Package redaction removes known secret shapes from text before memory
// material is persisted, rendered or indexed. It performs no I/O.
package redaction

import "slices"

// PolicyVersion identifies the detection rules; it is stored with every
// redacted copy as redaction_policy_version.
const PolicyVersion = "1"

// Kind names a detected secret shape. It never carries the value.
type Kind string

// Finding counts the occurrences of one kind replaced in a text.
type Finding struct {
	Kind  Kind
	Count int
}

// Result is the redacted text with what was replaced and the policy used.
// Findings hold one entry per kind, sorted by kind, and are empty for clean text.
type Result struct {
	Text          string
	Findings      []Finding
	PolicyVersion string
}

// Redactor redacts one text. Implementations must be deterministic.
type Redactor interface {
	Redact(text string) Result
}

// MergeFindings sums the counts per kind across several redactions of one
// piece of material, sorted by kind.
func MergeFindings(lists ...[]Finding) []Finding {
	var merged []Finding
	for _, list := range lists {
		for _, f := range list {
			if i := slices.IndexFunc(merged, func(m Finding) bool { return m.Kind == f.Kind }); i >= 0 {
				merged[i].Count += f.Count
			} else {
				merged = append(merged, f)
			}
		}
	}
	sortFindings(merged)
	return merged
}
