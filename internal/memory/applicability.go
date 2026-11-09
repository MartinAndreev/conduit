package memory

// SourceObservation is what the target workspace snapshot shows for a file.
type SourceObservation string

const (
	SourcePresent    SourceObservation = "present"
	SourceMissing    SourceObservation = "missing"
	SourceUnreadable SourceObservation = "unreadable"
)

// SourceCheck pairs a declared source with its target snapshot observation.
// Non-file sources need no observation.
type SourceCheck struct {
	Source       RecordSource
	Observation  SourceObservation
	ObservedHash string
}

type ApplicabilityFacts struct {
	Sources []SourceCheck
}

type ApplicabilityKind string

const (
	ApplicabilityApplicable        ApplicabilityKind = "applicable"
	ApplicabilityMismatch          ApplicabilityKind = "hash_mismatch"
	ApplicabilitySourceUnavailable ApplicabilityKind = "source_unavailable"
	ApplicabilityUnknown           ApplicabilityKind = "applicability_unknown"
	ApplicabilityInvalidFacts      ApplicabilityKind = "invalid_facts"
)

type ApplicabilityDecision struct {
	Kind   ApplicabilityKind
	Reason string
}

// DecideApplicability compares declared file hashes with the target snapshot,
// never commit ancestry, so squash, cherry-pick, revert and uncommitted changes
// resolve correctly. A proven difference precedes a source that cannot be read.
func DecideApplicability(f ApplicabilityFacts) ApplicabilityDecision {
	var fileSources int
	var mismatch, unavailable bool
	for _, check := range f.Sources {
		if check.Source.Kind != SourceFile {
			continue
		}
		fileSources++
		if check.Source.Path == "" || check.Source.BlobHash == "" {
			return ApplicabilityDecision{Kind: ApplicabilityInvalidFacts, Reason: "file source needs a path and blob hash"}
		}
		switch check.Observation {
		case SourcePresent:
			if check.ObservedHash == "" {
				return ApplicabilityDecision{Kind: ApplicabilityInvalidFacts, Reason: "present source needs an observed hash"}
			}
			mismatch = mismatch || check.ObservedHash != check.Source.BlobHash
		case SourceMissing, SourceUnreadable:
			unavailable = true
		default:
			return ApplicabilityDecision{Kind: ApplicabilityInvalidFacts, Reason: "unknown source observation"}
		}
	}
	switch {
	case fileSources == 0:
		return ApplicabilityDecision{Kind: ApplicabilityUnknown, Reason: "record has no file source"}
	case mismatch:
		return ApplicabilityDecision{Kind: ApplicabilityMismatch, Reason: "a declared source hash differs from the target snapshot"}
	case unavailable:
		return ApplicabilityDecision{Kind: ApplicabilitySourceUnavailable, Reason: "a declared source is missing or unreadable in the target snapshot"}
	default:
		return ApplicabilityDecision{Kind: ApplicabilityApplicable, Reason: "every declared source hash matches the target snapshot"}
	}
}
