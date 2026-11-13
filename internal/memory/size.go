package memory

// MaxMaterialBytes is the largest item text, in UTF-8 bytes, that composition
// accepts before redaction (initial value).
const MaxMaterialBytes = 32 * 1024

// MaxStoredContentBytes bounds a stored version's content and its normalized
// search text, each in UTF-8 bytes: 4 × MaxMaterialBytes. It is a storage cap,
// not the admission limit: a version above MaxMaterialBytes may be stored but
// is never admitted. The write path refuses text above it rather than
// truncating. Storage's CHECK states the same bound.
const MaxStoredContentBytes = 4 * MaxMaterialBytes

// MaxIdentityBytes bounds, in UTF-8 bytes, each stored identity value: IDs,
// content and blob hashes, author and reviewer, project, run and attempt,
// base commit, accepted result ref and redaction policy version. Storage's
// CHECKs state the same bound; a test keeps the two equal.
const MaxIdentityBytes = 256

// MaxTextFieldBytes bounds, in UTF-8 bytes, each stored free-text field: a
// source ref or path and a review reason. Storage's CHECKs state the same
// bound; a test keeps the two equal.
const MaxTextFieldBytes = 4096

// Write paths measure these bounds on the stored value, after redaction,
// which can lengthen text, and refuse a value above them rather than
// truncating it.

// tokenBytes is the estimate's bytes per token. Three bytes per token
// overestimates English text and stays close for Cyrillic, which takes two
// bytes per letter.
const tokenBytes = 3

// EstimateTokens is the budget's token estimate for exactly these bytes:
// ceil(UTF-8 bytes / 3). Composition applies it to the bytes it sends.
func EstimateTokens(text string) int64 {
	return (int64(len(text)) + tokenBytes - 1) / tokenBytes
}

// MaxSourcesPerItem is the most sources one item may carry (initial value).
const MaxSourcesPerItem = 32

// MaxSourceBytes bounds the UTF-8 bytes of all of one item's source refs, paths
// and blob hashes together (initial value). It also bounds every single value.
const MaxSourceBytes = 8 * 1024

// SourceBytes is the measure MaxSourceBytes applies to: the UTF-8 bytes of
// every source's ref, path and blob hash.
func SourceBytes(sources []RecordSource) int64 {
	var total int64
	for _, source := range sources {
		total += int64(len(source.Ref) + len(source.Path) + len(source.BlobHash))
	}
	return total
}

// MaterialSizeFacts describe one item as stored and as read, before
// redaction. A reader may withhold text or sources above the limits; within
// the limits it must have read them in full, so the read sizes equal the
// stored ones.
type MaterialSizeFacts struct {
	// Mandatory is true for material that must reach the agent in full.
	Mandatory bool
	// TextBytes is the stored text's size; LoadedTextBytes what was read.
	TextBytes       int64
	LoadedTextBytes int64
	// SourceCount and SourceBytes describe the stored sources; the Loaded
	// fields describe the ones read.
	SourceCount       int64
	LoadedSourceCount int64
	SourceBytes       int64
	LoadedSourceBytes int64
}

type MaterialSizeKind string

const (
	SizeWithinLimit MaterialSizeKind = "within_limit"
	// SizeOversized excludes optional material.
	SizeOversized MaterialSizeKind = "oversized"
	// SizeBlockedOversizedMandatory blocks the whole composition: mandatory
	// material is never truncated.
	SizeBlockedOversizedMandatory MaterialSizeKind = "blocked_oversized_mandatory"
	SizeInvalidFacts              MaterialSizeKind = "invalid_facts"
)

type MaterialSizeDecision struct {
	Kind   MaterialSizeKind
	Reason string
}

// DecideMaterialSize bounds an item's text and source metadata before
// redaction, so the redactor and the budget never see unbounded input. An
// item within the limits whose read sizes differ from the stored ones has
// partial facts and is never admitted.
func DecideMaterialSize(f MaterialSizeFacts) MaterialSizeDecision {
	if f.TextBytes < 0 || f.LoadedTextBytes < 0 || f.SourceCount < 0 || f.LoadedSourceCount < 0 || f.SourceBytes < 0 || f.LoadedSourceBytes < 0 {
		return MaterialSizeDecision{Kind: SizeInvalidFacts, Reason: "a size is negative"}
	}
	reason := ""
	switch {
	case f.TextBytes > MaxMaterialBytes:
		reason = "text exceeds the per-item limit"
	case f.SourceCount > MaxSourcesPerItem:
		reason = "the item has more sources than the per-item limit"
	case f.SourceBytes > MaxSourceBytes:
		reason = "source references exceed the per-item limit"
	}
	switch {
	case reason != "" && f.Mandatory:
		return MaterialSizeDecision{Kind: SizeBlockedOversizedMandatory, Reason: "mandatory material cannot be truncated: " + reason}
	case reason != "":
		return MaterialSizeDecision{Kind: SizeOversized, Reason: "optional material excluded: " + reason}
	case f.LoadedTextBytes != f.TextBytes || f.LoadedSourceCount != f.SourceCount || f.LoadedSourceBytes != f.SourceBytes:
		return MaterialSizeDecision{Kind: SizeInvalidFacts, Reason: "material within the limits was not read in full"}
	}
	return MaterialSizeDecision{Kind: SizeWithinLimit, Reason: "text and sources are within the per-item limits"}
}
