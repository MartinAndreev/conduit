package memory

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text string
		want int64
	}{
		{"", 0},
		{"a", 1},
		{"abc", 1},
		{"abcd", 2},
		{"абв", 2}, // six bytes
		{strings.Repeat("x", MaxMaterialBytes), 10923},
	}
	for _, tt := range tests {
		if got := EstimateTokens(tt.text); got != tt.want {
			t.Errorf("EstimateTokens(%d bytes) = %d, want %d", len(tt.text), got, tt.want)
		}
	}
}

// read describes material read in full.
func read(mandatory bool, text, sources, sourceBytes int64) MaterialSizeFacts {
	return MaterialSizeFacts{Mandatory: mandatory, TextBytes: text, LoadedTextBytes: text, SourceCount: sources, LoadedSourceCount: sources, SourceBytes: sourceBytes, LoadedSourceBytes: sourceBytes}
}

func TestDecideMaterialSize(t *testing.T) {
	withheld := func(f MaterialSizeFacts) MaterialSizeFacts {
		f.LoadedTextBytes, f.LoadedSourceCount, f.LoadedSourceBytes = 0, 0, 0
		return f
	}
	change := func(f MaterialSizeFacts, edit func(*MaterialSizeFacts)) MaterialSizeFacts {
		edit(&f)
		return f
	}
	tests := []struct {
		name  string
		facts MaterialSizeFacts
		want  MaterialSizeKind
	}{
		{"empty", MaterialSizeFacts{}, SizeWithinLimit},
		{"optional at limits", read(false, MaxMaterialBytes, MaxSourcesPerItem, MaxSourceBytes), SizeWithinLimit},
		{"mandatory at limits", read(true, MaxMaterialBytes, MaxSourcesPerItem, MaxSourceBytes), SizeWithinLimit},
		{"optional text one byte over", read(false, MaxMaterialBytes+1, 0, 0), SizeOversized},
		{"mandatory text one byte over", read(true, MaxMaterialBytes+1, 0, 0), SizeBlockedOversizedMandatory},
		{"optional one source over", read(false, 1, MaxSourcesPerItem+1, 10), SizeOversized},
		{"mandatory one source over", read(true, 1, MaxSourcesPerItem+1, 10), SizeBlockedOversizedMandatory},
		{"optional source bytes one over", read(false, 1, 1, MaxSourceBytes+1), SizeOversized},
		{"mandatory source bytes one over", read(true, 1, 1, MaxSourceBytes+1), SizeBlockedOversizedMandatory},
		{"withheld oversized text", withheld(read(false, MaxMaterialBytes+1, 1, 10)), SizeOversized},
		{"withheld oversized sources", withheld(read(true, 10, 1, MaxSourceBytes+1)), SizeBlockedOversizedMandatory},
		{"text within limit not read", change(read(false, 10, 0, 0), func(f *MaterialSizeFacts) { f.LoadedTextBytes = 0 }), SizeInvalidFacts},
		{"mandatory text partly read", change(read(true, 10, 0, 0), func(f *MaterialSizeFacts) { f.LoadedTextBytes = 9 }), SizeInvalidFacts},
		{"sources within limit not read", change(read(false, 10, 2, 20), func(f *MaterialSizeFacts) { f.LoadedSourceCount, f.LoadedSourceBytes = 0, 0 }), SizeInvalidFacts},
		{"source bytes differ", change(read(false, 10, 2, 20), func(f *MaterialSizeFacts) { f.LoadedSourceBytes = 19 }), SizeInvalidFacts},
		{"negative text", MaterialSizeFacts{TextBytes: -1}, SizeInvalidFacts},
		{"negative loaded text mandatory", MaterialSizeFacts{Mandatory: true, LoadedTextBytes: -1}, SizeInvalidFacts},
		{"negative source count", MaterialSizeFacts{SourceCount: -1}, SizeInvalidFacts},
		{"negative loaded source count", MaterialSizeFacts{LoadedSourceCount: -1}, SizeInvalidFacts},
		{"negative source bytes", MaterialSizeFacts{SourceBytes: -1}, SizeInvalidFacts},
		{"negative loaded source bytes", MaterialSizeFacts{LoadedSourceBytes: -1}, SizeInvalidFacts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideMaterialSize(tt.facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want kind %q with a reason", got, tt.want)
			}
		})
	}
}

// "спец" is 8 UTF-8 bytes, "a/b.go" 6 and "blob" 4.
func TestSourceBytesCountsEveryValue(t *testing.T) {
	sources := []RecordSource{{Kind: SourceSpec, Ref: "спец"}, {Kind: SourceFile, Path: "a/b.go", BlobHash: "blob"}}
	if got := SourceBytes(sources); got != 18 {
		t.Fatalf("SourceBytes = %d", got)
	}
	if SourceBytes(nil) != 0 {
		t.Fatal("no sources measured as non-zero")
	}
}
