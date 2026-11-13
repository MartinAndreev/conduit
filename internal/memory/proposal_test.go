package memory

import (
	"slices"
	"strings"
	"testing"
)

func validProposalEntry() ProposalKeepFacts {
	return ProposalKeepFacts{
		Decoded:  true,
		Category: CategoryDecision,
		Content:  "Миграциите са адитивни",
		Sources:  []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "0fd0bcfb44f83e7d5ac7a8922578276b9af48746"}, {Kind: SourceSpec, Ref: "specs/012"}},
	}
}

func specSources(n int) []RecordSource {
	sources := make([]RecordSource, n)
	for index := range sources {
		sources[index] = RecordSource{Kind: SourceSpec, Ref: "specs/012"}
	}
	return sources
}

func TestDecideProposalKeep(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ProposalKeepFacts)
		want   ProposalKeepKind
	}{
		{"valid", func(*ProposalKeepFacts) {}, ProposalKeep},
		{"valid without sources", func(f *ProposalKeepFacts) { f.Sources = nil }, ProposalKeep},
		{"last place", func(f *ProposalKeepFacts) { f.Kept = MaxProposalsPerReport - 1 }, ProposalKeep},
		{"negative kept", func(f *ProposalKeepFacts) { f.Kept = -1 }, ProposalInvalidFacts},
		{"kept above limit", func(f *ProposalKeepFacts) { f.Kept = MaxProposalsPerReport + 1 }, ProposalInvalidFacts},
		{"invalid facts precede malformed", func(f *ProposalKeepFacts) { f.Kept, f.Decoded = -1, false }, ProposalInvalidFacts},
		{"not decoded", func(f *ProposalKeepFacts) { f.Decoded = false }, ProposalMalformedEntry},
		{"malformed precedes category", func(f *ProposalKeepFacts) { f.Decoded, f.Category = false, "" }, ProposalMalformedEntry},
		{"unknown category", func(f *ProposalKeepFacts) { f.Category = "architecture" }, ProposalInvalidCategory},
		{"missing category", func(f *ProposalKeepFacts) { f.Category = "" }, ProposalInvalidCategory},
		{"category precedes source", func(f *ProposalKeepFacts) {
			f.Category, f.Sources = "", []RecordSource{{Kind: "url"}}
		}, ProposalInvalidCategory},
		{"unknown source kind", func(f *ProposalKeepFacts) { f.Sources = []RecordSource{{Kind: "url", Ref: "x"}} }, ProposalInvalidSource},
		{"file without hash", func(f *ProposalKeepFacts) { f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go"}} }, ProposalInvalidSource},
		{"file without path", func(f *ProposalKeepFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, BlobHash: "e9d71f5ee7c92d6dc9e92ffdad17b8bd49418f98"}}
		}, ProposalInvalidSource},
		{"repeated file path", func(f *ProposalKeepFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "e9d71f5ee7c92d6dc9e92ffdad17b8bd49418f98"}, {Kind: SourceFile, Path: "a.go", BlobHash: "84a516841ba77a5b4648de2cd0dfcb30ea46dbb4"}}
		}, ProposalInvalidSource},
		{"spec with path", func(f *ProposalKeepFacts) { f.Sources = []RecordSource{{Kind: SourceSpec, Ref: "s", Path: "p"}} }, ProposalInvalidSource},
		{"source precedes content", func(f *ProposalKeepFacts) {
			f.Sources, f.Content = []RecordSource{{Kind: SourceEvent}}, ""
		}, ProposalInvalidSource},
		{"empty content", func(f *ProposalKeepFacts) { f.Content = " \n\t" }, ProposalEmptyContent},
		{"empty content precedes duplicate", func(f *ProposalKeepFacts) { f.Content, f.RepeatsEarlierEntry = "", true }, ProposalEmptyContent},
		{"duplicate", func(f *ProposalKeepFacts) { f.RepeatsEarlierEntry = true }, ProposalDuplicate},
		{"duplicate precedes limit", func(f *ProposalKeepFacts) {
			f.RepeatsEarlierEntry, f.Kept = true, MaxProposalsPerReport
		}, ProposalDuplicate},
		{"over limit", func(f *ProposalKeepFacts) { f.Kept = MaxProposalsPerReport }, ProposalOverLimit},
		{"negative position", func(f *ProposalKeepFacts) { f.Position = -1 }, ProposalInvalidFacts},
		{"last examined position", func(f *ProposalKeepFacts) { f.Position = MaxProposalEntriesExamined - 1 }, ProposalKeep},
		{"past the examined entries", func(f *ProposalKeepFacts) { f.Position = MaxProposalEntriesExamined }, ProposalOverLimit},
		{"unexamined precedes malformed", func(f *ProposalKeepFacts) { *f = ProposalKeepFacts{Position: MaxProposalEntriesExamined} }, ProposalOverLimit},
		{"sources at the limit", func(f *ProposalKeepFacts) { f.Sources = specSources(MaxSourcesPerItem) }, ProposalKeep},
		{"sources over the limit", func(f *ProposalKeepFacts) { f.Sources = specSources(MaxSourcesPerItem + 1) }, ProposalInvalidSource},
		{"declared count over the limit without sources", func(f *ProposalKeepFacts) { f.Sources, f.SourceCount = nil, MaxSourcesPerItem+1 }, ProposalInvalidSource},
		{"declared count at the limit", func(f *ProposalKeepFacts) { f.SourceCount = MaxSourcesPerItem }, ProposalKeep},
		{"category precedes the source count", func(f *ProposalKeepFacts) { f.Category, f.SourceCount = "", MaxSourcesPerItem+1 }, ProposalInvalidCategory},
		{"negative source count", func(f *ProposalKeepFacts) { f.SourceCount = -1 }, ProposalInvalidFacts},
		{"secret as blob hash", func(f *ProposalKeepFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "password=hunter2"}}
		}, ProposalInvalidSource},
		{"over-long path is the creation's", func(f *ProposalKeepFacts) {
			f.Sources = []RecordSource{{Kind: SourceSpec, Ref: strings.Repeat("r", MaxTextFieldBytes+1)}}
		}, ProposalKeep},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts := validProposalEntry()
			tt.change(&facts)
			got := DecideProposalKeep(facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestProposalKeepKindsAreDistinctAndEndWithKeep(t *testing.T) {
	kinds := ProposalKeepKinds()
	if len(slices.Compact(slices.Sorted(slices.Values(kinds)))) != len(kinds) || kinds[len(kinds)-1] != ProposalKeep {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestDecideProposalMerge(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want ProposalMergeKind
	}{
		{"two", []string{"r1", "r2"}, ProposalMergeAllowed},
		{"three", []string{"r1", "r2", "r3"}, ProposalMergeAllowed},
		{"empty id", []string{"r1", ""}, ProposalMergeInvalidFacts},
		{"empty id precedes too few", []string{""}, ProposalMergeInvalidFacts},
		{"none", nil, ProposalMergeTooFew},
		{"one", []string{"r1"}, ProposalMergeTooFew},
		{"repeated", []string{"r1", "r2", "r1"}, ProposalMergeRepeated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideProposalMerge(ProposalMergeFacts{RecordIDs: tt.ids})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestMergedReasonNamesTheRecord(t *testing.T) {
	if got := MergedReason("record-7"); got != "merged into record record-7" {
		t.Fatalf("reason = %q", got)
	}
}

func TestSameSearchText(t *testing.T) {
	identity := SearchTextIdentity{NormalizationVersion: 1, Digest: "d"}
	tests := []struct {
		name        string
		left, right SearchTextIdentity
		want        bool
	}{
		{"equal", identity, identity, true},
		{"other digest", identity, SearchTextIdentity{NormalizationVersion: 1, Digest: "e"}, false},
		{"other normalization version", identity, SearchTextIdentity{NormalizationVersion: 2, Digest: "d"}, false},
		{"no version", SearchTextIdentity{Digest: "d"}, SearchTextIdentity{Digest: "d"}, false},
		{"no digest", SearchTextIdentity{NormalizationVersion: 1}, SearchTextIdentity{NormalizationVersion: 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameSearchText(tt.left, tt.right); got != tt.want {
				t.Fatalf("SameSearchText = %v", got)
			}
		})
	}
}
