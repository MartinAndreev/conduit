package memory

import (
	"slices"
	"strings"
)

// MaxProposalsPerReport is how many proposals of one agent report are kept,
// in report order (initial value). The rest are dropped and counted.
const MaxProposalsPerReport = 5

// MaxProposalEntriesExamined is how many entries of one report's block are
// decoded and decided on their merits (initial value). Every later entry is
// dropped as over the limit without being decoded, so the work per report is
// bounded whatever the block holds.
const MaxProposalEntriesExamined = 100

// MaxReportBytes bounds an agent report, in bytes of the raw report, that
// proposals are read from (Martin, 2026-11-13). A larger report is refused as
// a whole before it is scanned, decoded or redacted; exactly MaxReportBytes
// is read.
const MaxReportBytes = 256 << 10

// ProposalKeepFacts describe one entry of an agent report's proposal block,
// as decoded from the raw report with its content, refs and paths redacted
// after decoding, and what the entries before it in the same report left.
type ProposalKeepFacts struct {
	// Position is the entry's 0-based position in the block. An entry at or
	// after MaxProposalEntriesExamined is not decoded; its other facts are
	// zero.
	Position int
	// Decoded is false when the entry is not a JSON object holding only the
	// format's fields with their types.
	Decoded  bool
	Category RecordCategory
	Content  string
	Sources  []RecordSource
	// SourceCount is how many sources the entry declares. An entry over
	// MaxSourcesPerItem may be decided without its Sources being copied or
	// redacted; the larger of SourceCount and len(Sources) is compared.
	SourceCount int
	// RepeatsEarlierEntry is true when an earlier entry of the same report,
	// kept or dropped only for the limit, has the same normalized search
	// text (SameSearchText).
	RepeatsEarlierEntry bool
	// Kept counts the entries of the report kept before this one.
	Kept int
}

type ProposalKeepKind string

const (
	ProposalKeep         ProposalKeepKind = "keep"
	ProposalInvalidFacts ProposalKeepKind = "invalid_facts"
	// ProposalMalformedEntry drops an entry that is not an object of the
	// format's fields: an unknown field, a wrong type or a non-object.
	ProposalMalformedEntry  ProposalKeepKind = "malformed_entry"
	ProposalInvalidCategory ProposalKeepKind = "invalid_category"
	ProposalInvalidSource   ProposalKeepKind = "invalid_source"
	ProposalEmptyContent    ProposalKeepKind = "empty_content"
	// ProposalDuplicate drops an entry whose normalized text repeats an
	// earlier one of the same report.
	ProposalDuplicate ProposalKeepKind = "duplicate"
	// ProposalOverLimit drops a valid entry after MaxProposalsPerReport were
	// kept, and any entry at or after MaxProposalEntriesExamined.
	ProposalOverLimit ProposalKeepKind = "over_limit"
)

// ProposalKeepKinds lists every kind in DecideProposalKeep's order. Every
// kind but ProposalKeep drops the entry.
func ProposalKeepKinds() []ProposalKeepKind {
	return []ProposalKeepKind{
		ProposalInvalidFacts, ProposalMalformedEntry, ProposalInvalidCategory, ProposalInvalidSource,
		ProposalEmptyContent, ProposalDuplicate, ProposalOverLimit, ProposalKeep,
	}
}

type ProposalKeepDecision struct {
	Kind   ProposalKeepKind
	Reason string
}

// DecideProposalKeep answers whether one entry of a report's proposal block
// is kept as a proposal. Reasons never repeat a value from the report.
//
// Order: invalid facts (a negative or impossible kept count, a negative
// position or source count); a position at or after MaxProposalEntriesExamined (over_limit);
// an entry that did not decode; an unknown category; more than
// MaxSourcesPerItem sources, then an invalid source shape (that of
// DecideRecordCreation, including the blob hash grammar; the ref and path
// storage bounds are the creation's, so an over-long value becomes that
// proposal's creation refusal); empty content; a repeat of an earlier entry's
// normalized text; MaxProposalsPerReport already kept; keep. Invalid and
// repeated entries therefore never take one of the kept places.
func DecideProposalKeep(f ProposalKeepFacts) ProposalKeepDecision {
	switch {
	case f.Kept < 0 || f.Kept > MaxProposalsPerReport || f.Position < 0 || f.SourceCount < 0:
		return ProposalKeepDecision{Kind: ProposalInvalidFacts, Reason: "kept count is outside the report limit or the position is negative"}
	case f.Position >= MaxProposalEntriesExamined:
		return ProposalKeepDecision{Kind: ProposalOverLimit, Reason: "the entry is past the entries a report may have examined"}
	case !f.Decoded:
		return ProposalKeepDecision{Kind: ProposalMalformedEntry, Reason: "entry is not an object of the proposal format's fields"}
	case !slices.Contains(Categories(), f.Category):
		return ProposalKeepDecision{Kind: ProposalInvalidCategory, Reason: "category is not one of the fixed categories"}
	}
	if max(f.SourceCount, len(f.Sources)) > MaxSourcesPerItem {
		return ProposalKeepDecision{Kind: ProposalInvalidSource, Reason: "the entry has more sources than the per-item limit"}
	}
	if reason := invalidCreationSources(f.Sources); reason != "" {
		return ProposalKeepDecision{Kind: ProposalInvalidSource, Reason: reason}
	}
	switch {
	case strings.TrimSpace(f.Content) == "":
		return ProposalKeepDecision{Kind: ProposalEmptyContent, Reason: "content is empty"}
	case f.RepeatsEarlierEntry:
		return ProposalKeepDecision{Kind: ProposalDuplicate, Reason: "normalized content repeats an earlier entry of the report"}
	case f.Kept == MaxProposalsPerReport:
		return ProposalKeepDecision{Kind: ProposalOverLimit, Reason: "the report's proposal limit is reached"}
	}
	return ProposalKeepDecision{Kind: ProposalKeep, Reason: "entry is a valid proposal within the report limit"}
}

// ProposalMergeFacts are the proposals a human merge names, in request order.
type ProposalMergeFacts struct {
	RecordIDs []string
}

type ProposalMergeKind string

const (
	ProposalMergeAllowed      ProposalMergeKind = "merge"
	ProposalMergeInvalidFacts ProposalMergeKind = "invalid_facts"
	ProposalMergeTooFew       ProposalMergeKind = "too_few_proposals"
	ProposalMergeRepeated     ProposalMergeKind = "repeated_proposal"
)

type ProposalMergeDecision struct {
	Kind   ProposalMergeKind
	Reason string
}

// MinMergedProposals is the fewest proposals a merge names.
const MinMergedProposals = 2

// DecideProposalMerge answers whether a merge names a usable set of
// proposals. Each proposal's own transition (its project, version, status
// and content) is DecideRecordTransition's, and the merged text's is
// DecideRecordCreation's.
//
// Order: an empty record ID; fewer than MinMergedProposals; a proposal named
// twice; merge.
func DecideProposalMerge(f ProposalMergeFacts) ProposalMergeDecision {
	if slices.Contains(f.RecordIDs, "") {
		return ProposalMergeDecision{Kind: ProposalMergeInvalidFacts, Reason: "every merged proposal needs a record ID"}
	}
	if len(f.RecordIDs) < MinMergedProposals {
		return ProposalMergeDecision{Kind: ProposalMergeTooFew, Reason: "a merge names at least two proposals"}
	}
	seen := make(map[string]bool, len(f.RecordIDs))
	for _, recordID := range f.RecordIDs {
		if seen[recordID] {
			return ProposalMergeDecision{Kind: ProposalMergeRepeated, Reason: "a proposal is named more than once"}
		}
		seen[recordID] = true
	}
	return ProposalMergeDecision{Kind: ProposalMergeAllowed, Reason: "the merge names distinct proposals"}
}

// MergedReason is the system-generated reason of a proposal rejected by a
// merge. It names only the new record, never proposal text.
func MergedReason(recordID string) string {
	return "merged into record " + recordID
}
