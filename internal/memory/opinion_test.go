package memory

import "testing"

func opinionFacts(change ...func(*RecordOpinionFacts)) RecordOpinionFacts {
	f := RecordOpinionFacts{
		RequestProjectID:  "p1",
		RequestRecordID:   "r1",
		VersionID:         "v1",
		ContentHash:       "c1",
		Actor:             ActorAgent,
		Reviewer:          "auditor",
		Reason:            "duplicates r2",
		RecordFound:       true,
		Record:            Record{ID: "r1", ProjectID: "p1", Status: StatusActive, CurrentVersionID: "v2", Version: 4},
		VersionFound:      true,
		StoredContentHash: "c1",
	}
	for _, c := range change {
		c(&f)
	}
	return f
}

func TestDecideRecordOpinion(t *testing.T) {
	type change = func(*RecordOpinionFacts)
	deleted := func(f *RecordOpinionFacts) { f.ContentDeleted = true }
	tests := []struct {
		name    string
		changes []change
		want    RecordOpinionKind
	}{
		{"agent opinion on an older version", nil, OpinionAdd},
		{"system opinion", []change{func(f *RecordOpinionFacts) { f.Actor = ActorSystem }}, OpinionAdd},
		{"opinion without reason", []change{func(f *RecordOpinionFacts) { f.Reason = "" }}, OpinionAdd},
		{"opinion on a terminal record", []change{func(f *RecordOpinionFacts) { f.Record.Status = StatusArchived }}, OpinionAdd},
		{"opinion without reason on deleted content", []change{deleted, func(f *RecordOpinionFacts) { f.Reason = "" }}, OpinionAdd},

		{"no project", []change{func(f *RecordOpinionFacts) { f.RequestProjectID = "" }}, OpinionInvalidFacts},
		{"no record", []change{func(f *RecordOpinionFacts) { f.RequestRecordID = "" }}, OpinionInvalidFacts},
		{"no version", []change{func(f *RecordOpinionFacts) { f.VersionID = "" }}, OpinionInvalidFacts},
		{"no content hash", []change{func(f *RecordOpinionFacts) { f.ContentHash = "" }}, OpinionInvalidFacts},
		{"no reviewer", []change{func(f *RecordOpinionFacts) { f.Reviewer = "" }}, OpinionInvalidFacts},
		{"unknown actor", []change{func(f *RecordOpinionFacts) { f.Actor = "operator" }}, OpinionInvalidFacts},

		{"human through the opinion path", []change{func(f *RecordOpinionFacts) { f.Actor = ActorHuman }}, OpinionActorNotAllowed},
		{"actor precedes not found", []change{func(f *RecordOpinionFacts) { f.Actor, f.RecordFound = ActorHuman, false }}, OpinionActorNotAllowed},

		{"missing record", []change{func(f *RecordOpinionFacts) { f.RecordFound, f.Record = false, Record{} }}, OpinionNotFound},
		{"record of another project", []change{func(f *RecordOpinionFacts) { f.Record.ProjectID = "p2" }}, OpinionNotFound},
		{"other record read", []change{func(f *RecordOpinionFacts) { f.Record.ID = "r2" }}, OpinionNotFound},
		{"version of another record", []change{func(f *RecordOpinionFacts) { f.VersionFound = false }}, OpinionNotFound},

		{"other content hash", []change{func(f *RecordOpinionFacts) { f.ContentHash = "c0" }}, OpinionHashMismatch},
		{"hash precedes deleted content", []change{deleted, func(f *RecordOpinionFacts) { f.ContentHash = "c0" }}, OpinionHashMismatch},

		{"reason on deleted content", []change{deleted}, OpinionContentDeleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideRecordOpinion(opinionFacts(tt.changes...))
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}
