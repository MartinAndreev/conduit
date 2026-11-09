package memory

import "testing"

func fileCheck(declared string, observation SourceObservation, observed string) SourceCheck {
	return SourceCheck{Source: RecordSource{Kind: SourceFile, Path: "a.go", BlobHash: declared}, Observation: observation, ObservedHash: observed}
}

func TestDecideApplicability(t *testing.T) {
	match := fileCheck("h", SourcePresent, "h")
	differs := fileCheck("h", SourcePresent, "other")
	missing := fileCheck("h", SourceMissing, "")
	unreadable := fileCheck("h", SourceUnreadable, "")
	event := SourceCheck{Source: RecordSource{Kind: SourceEvent, Ref: "e1"}}
	tests := []struct {
		name    string
		sources []SourceCheck
		want    ApplicabilityKind
	}{
		{"no sources", nil, ApplicabilityUnknown},
		{"only non-file sources", []SourceCheck{event}, ApplicabilityUnknown},
		{"all hashes match", []SourceCheck{match, match}, ApplicabilityApplicable},
		{"event beside matching file", []SourceCheck{event, match}, ApplicabilityApplicable},
		{"one hash differs", []SourceCheck{match, differs}, ApplicabilityMismatch},
		{"missing file", []SourceCheck{match, missing}, ApplicabilitySourceUnavailable},
		{"unreadable file", []SourceCheck{unreadable}, ApplicabilitySourceUnavailable},
		{"mismatch precedes unavailable", []SourceCheck{missing, differs}, ApplicabilityMismatch},
		{"file without hash", []SourceCheck{fileCheck("", SourcePresent, "h")}, ApplicabilityInvalidFacts},
		{"file without path", []SourceCheck{{Source: RecordSource{Kind: SourceFile, BlobHash: "h"}, Observation: SourcePresent, ObservedHash: "h"}}, ApplicabilityInvalidFacts},
		{"present without observed hash", []SourceCheck{fileCheck("h", SourcePresent, "")}, ApplicabilityInvalidFacts},
		{"unknown observation", []SourceCheck{fileCheck("h", "", "h")}, ApplicabilityInvalidFacts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideApplicability(ApplicabilityFacts{Sources: tt.sources})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}
