package memory

import (
	"strings"
	"testing"
)

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
	// suggest names the current version v1 with the suggestion.
	suggest := func(suggestion Suggestion) change {
		return func(f *RecordOpinionFacts) { f.Suggestion, f.Record.CurrentVersionID = suggestion, "v1" }
	}
	status := func(status RecordStatus) change { return func(f *RecordOpinionFacts) { f.Record.Status = status } }
	dismissed := func(f *RecordOpinionFacts) { f.SuggestionDismissed = true }
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
		{"reviewer one byte over", []change{func(f *RecordOpinionFacts) { f.Reviewer = strings.Repeat("ж", MaxIdentityBytes/2) + "x" }}, OpinionInvalidFacts},
		{"reviewer at the bound", []change{func(f *RecordOpinionFacts) { f.Reviewer = strings.Repeat("ж", MaxIdentityBytes/2) }}, OpinionAdd},
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
		{"deleted content precedes a long reason", []change{deleted, func(f *RecordOpinionFacts) { f.Reason = strings.Repeat("ж", MaxTextFieldBytes) }}, OpinionContentDeleted},

		{"reason at the bound", []change{func(f *RecordOpinionFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes) }}, OpinionAdd},
		{"reason one byte over", []change{func(f *RecordOpinionFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes+1) }}, OpinionReasonTooLong},
		{"reason measured in bytes", []change{func(f *RecordOpinionFacts) { f.Reason = strings.Repeat("ж", MaxTextFieldBytes/2) + "r" }}, OpinionReasonTooLong},

		{"archive suggestion on the current active version", []change{suggest(SuggestArchive)}, OpinionAdd},
		{"archive suggestion on a stale record", []change{suggest(SuggestArchive), status(StatusNeedsVerification)}, OpinionAdd},
		{"delete suggestion on the current version", []change{suggest(SuggestDelete)}, OpinionAdd},
		{"delete suggestion on an archived record", []change{suggest(SuggestDelete), status(StatusArchived)}, OpinionAdd},
		{"delete suggestion on a proposal", []change{suggest(SuggestDelete), status(StatusProposed)}, OpinionAdd},
		{"unknown suggestion", []change{suggest("forget")}, OpinionInvalidFacts},
		{"unknown suggestion precedes the actor", []change{suggest("forget"), func(f *RecordOpinionFacts) { f.Actor = ActorHuman }}, OpinionInvalidFacts},
		{"system suggestion", []change{suggest(SuggestArchive), func(f *RecordOpinionFacts) { f.Actor = ActorSystem }}, OpinionActorNotAllowed},
		{"human suggestion", []change{suggest(SuggestDelete), func(f *RecordOpinionFacts) { f.Actor = ActorHuman }}, OpinionActorNotAllowed},
		{"suggestion actor precedes not found", []change{suggest(SuggestDelete), func(f *RecordOpinionFacts) { f.Actor, f.RecordFound = ActorSystem, false }}, OpinionActorNotAllowed},
		{"suggestion on another project", []change{suggest(SuggestArchive), func(f *RecordOpinionFacts) { f.Record.ProjectID = "p2" }}, OpinionNotFound},
		{"suggestion with another hash", []change{suggest(SuggestArchive), func(f *RecordOpinionFacts) { f.ContentHash = "c0" }}, OpinionHashMismatch},
		{"suggestion without reason on deleted content", []change{suggest(SuggestDelete), deleted, func(f *RecordOpinionFacts) { f.Reason = "" }}, OpinionContentDeleted},
		{"deleted content precedes a missing reason", []change{suggest(SuggestArchive), deleted, func(f *RecordOpinionFacts) { f.Reason = " " }}, OpinionContentDeleted},
		{"suggestion without reason", []change{suggest(SuggestArchive), func(f *RecordOpinionFacts) { f.Reason = "" }}, OpinionReasonRequired},
		{"suggestion with a blank reason", []change{suggest(SuggestDelete), func(f *RecordOpinionFacts) { f.Reason = " \n\t" }}, OpinionReasonRequired},
		{"missing reason precedes an old version", []change{suggest(SuggestDelete), func(f *RecordOpinionFacts) { f.Reason, f.Record.CurrentVersionID = "", "v9" }}, OpinionReasonRequired},
		{"suggestion reason one byte over", []change{suggest(SuggestArchive), func(f *RecordOpinionFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes+1) }}, OpinionReasonTooLong},
		{"long reason precedes an old version", []change{suggest(SuggestArchive), func(f *RecordOpinionFacts) {
			f.Reason, f.Record.CurrentVersionID = strings.Repeat("r", MaxTextFieldBytes+1), "v9"
		}}, OpinionReasonTooLong},
		{"suggestion on an older version", []change{suggest(SuggestArchive), func(f *RecordOpinionFacts) { f.Record.CurrentVersionID = "v9" }}, OpinionVersionNotCurrent},
		{"old version precedes the status", []change{suggest(SuggestArchive), status(StatusArchived), func(f *RecordOpinionFacts) { f.Record.CurrentVersionID = "v9" }}, OpinionVersionNotCurrent},
		{"archive suggestion on an archived record", []change{suggest(SuggestArchive), status(StatusArchived)}, OpinionStatusNotApplicable},
		{"archive suggestion on a proposal", []change{suggest(SuggestArchive), status(StatusProposed)}, OpinionStatusNotApplicable},
		{"archive suggestion on a superseded record", []change{suggest(SuggestArchive), status(StatusSuperseded)}, OpinionStatusNotApplicable},
		{"delete suggestion on an unknown status", []change{suggest(SuggestDelete), status("lost")}, OpinionStatusNotApplicable},

		{"dismissed archive suggestion", []change{suggest(SuggestArchive), dismissed}, OpinionSuggestionDismissed},
		{"dismissed delete suggestion", []change{suggest(SuggestDelete), dismissed}, OpinionSuggestionDismissed},
		{"old version precedes the dismissal", []change{suggest(SuggestArchive), dismissed, func(f *RecordOpinionFacts) { f.Record.CurrentVersionID = "v9" }}, OpinionVersionNotCurrent},
		{"dismissal precedes the status", []change{suggest(SuggestArchive), dismissed, status(StatusArchived)}, OpinionSuggestionDismissed},
		{"a dismissal does not refuse a plain opinion", []change{dismissed}, OpinionAdd},
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
