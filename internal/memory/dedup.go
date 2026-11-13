package memory

// DeduplicationSubject is the scope of one side of a comparison.
type DeduplicationSubject struct {
	ProjectID  string
	Visibility Visibility
	AttemptID  string
}

// SearchTextIdentity identifies a normalized search text: the version of the
// normalization that produced it and a digest storage computes from it.
type SearchTextIdentity struct {
	NormalizationVersion int64
	Digest               string
}

// SameSearchText is the duplicate match (Martin, 2026-11-12): exact equality
// of normalized search text under the same normalization version. Texts
// normalized by different versions never match, and an identity without a
// version or digest matches nothing.
func SameSearchText(left, right SearchTextIdentity) bool {
	return left.NormalizationVersion > 0 && left.Digest != "" && left == right
}

// DeduplicationFacts compare two proposals. ContentMatches is SameSearchText
// of their current versions.
type DeduplicationFacts struct {
	Left           DeduplicationSubject
	Right          DeduplicationSubject
	ContentMatches bool
}

type DeduplicationKind string

const (
	DeduplicationDistinct     DeduplicationKind = "distinct"
	DeduplicationDuplicate    DeduplicationKind = "duplicate"
	DeduplicationMergeHint    DeduplicationKind = "merge_hint"
	DeduplicationInvalidFacts DeduplicationKind = "invalid_facts"
)

// DeduplicationDecision deliberately carries no trust: trust never transfers
// through deduplication. A duplicate links proposals; each one and its decision
// is kept.
type DeduplicationDecision struct {
	Kind   DeduplicationKind
	Reason string
}

// DecideDeduplication answers how two matching proposals relate. Only the same
// project and visibility can be duplicates, and different attempts are different
// scopes; across scopes a match is at most a hint for a human merge.
func DecideDeduplication(f DeduplicationFacts) DeduplicationDecision {
	if !validDeduplicationSubject(f.Left) || !validDeduplicationSubject(f.Right) {
		return DeduplicationDecision{Kind: DeduplicationInvalidFacts, Reason: "project or visibility is missing, or attempt scope has no attempt"}
	}
	if !f.ContentMatches {
		return DeduplicationDecision{Kind: DeduplicationDistinct, Reason: "content does not match"}
	}
	if f.Left.ProjectID != f.Right.ProjectID || f.Left.Visibility != f.Right.Visibility ||
		(f.Left.Visibility == VisibilityAttempt && f.Left.AttemptID != f.Right.AttemptID) {
		return DeduplicationDecision{Kind: DeduplicationMergeHint, Reason: "match across scopes is at most a hint for human merging"}
	}
	return DeduplicationDecision{Kind: DeduplicationDuplicate, Reason: "content matches within one project and visibility"}
}

func validDeduplicationSubject(subject DeduplicationSubject) bool {
	return subject.ProjectID != "" && validVisibility(subject.Visibility) &&
		(subject.Visibility != VisibilityAttempt || subject.AttemptID != "")
}
