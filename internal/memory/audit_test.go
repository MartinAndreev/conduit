package memory

import (
	"slices"
	"strings"
	"testing"
)

const (
	auditHash  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	auditOther = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// The list is what storage's CHECK mirrors; it names every constant once.
func TestSuggestionEnumerations(t *testing.T) {
	if got := Suggestions(); !slices.Equal(got, []Suggestion{SuggestArchive, SuggestDelete}) {
		t.Fatalf("Suggestions() = %v", got)
	}
	if got := AuditedStatuses(); !slices.Equal(got, []RecordStatus{StatusActive, StatusNeedsVerification}) {
		t.Fatalf("AuditedStatuses() = %v", got)
	}
	want := []SuggestionKeepKind{
		SuggestionInvalidFacts, SuggestionMalformedEntry, SuggestionInvalidIdentity, SuggestionUnknownAction,
		SuggestionEmptyReason, SuggestionDuplicate, SuggestionOverLimit, SuggestionKeep,
	}
	if got := SuggestionKeepKinds(); !slices.Equal(got, want) {
		t.Fatalf("SuggestionKeepKinds() = %v", got)
	}
}

// Archive applies where the lifecycle lets a human archive; delete applies
// to every known status.
func TestSuggestionApplies(t *testing.T) {
	for _, status := range RecordStatuses() {
		archivable := status == StatusActive || status == StatusNeedsVerification
		if got := suggestionApplies(SuggestArchive, status); got != archivable {
			t.Errorf("archive on %s = %v", status, got)
		}
		if !suggestionApplies(SuggestDelete, status) {
			t.Errorf("delete on %s refused", status)
		}
	}
	if suggestionApplies(SuggestDelete, "lost") || suggestionApplies("forget", StatusActive) {
		t.Fatal("unknown status or suggestion applies")
	}
}

func TestValidIdentityAndContentHash(t *testing.T) {
	for id, want := range map[string]bool{
		"record-0a1b":                 true,
		"R_1":                         true,
		strings.Repeat("a", 128):      true,
		strings.Repeat("a", 129):      false,
		"":                            false,
		"-leading":                    false,
		"has space":                   false,
		"dotted.id":                   false,
		"кирилица":                    false,
		"line\nbreak":                 false,
		"user:password@host":          false,
		"ghp_AAAAAAAAAAAAAAAAAAAAAAA": true,
	} {
		if got := ValidIdentity(id); got != want {
			t.Errorf("ValidIdentity(%q) = %v", id, got)
		}
	}
	for hash, want := range map[string]bool{
		auditHash:                           true,
		ContentHash("text"):                 true,
		strings.ToUpper(auditHash):          false,
		"sha256:" + strings.Repeat("a", 63): false,
		"sha1:" + strings.Repeat("a", 64):   false,
		auditHash + "\n":                    false,
	} {
		if got := ValidContentHash(hash); got != want {
			t.Errorf("ValidContentHash(%q) = %v", hash, got)
		}
	}
}

func keepFacts(change ...func(*SuggestionKeepFacts)) SuggestionKeepFacts {
	f := SuggestionKeepFacts{
		Position: 3, Decoded: true,
		RecordID: "record-1", VersionID: "version-1", ContentHash: auditHash,
		Action: SuggestArchive, Reason: "superseded by the 2026 decision", Kept: 2,
	}
	for _, c := range change {
		c(&f)
	}
	return f
}

func TestDecideSuggestionKeep(t *testing.T) {
	type change = func(*SuggestionKeepFacts)
	tests := []struct {
		name    string
		changes []change
		want    SuggestionKeepKind
	}{
		{"valid archive", nil, SuggestionKeep},
		{"valid delete", []change{func(f *SuggestionKeepFacts) { f.Action = SuggestDelete }}, SuggestionKeep},
		{"first entry", []change{func(f *SuggestionKeepFacts) { f.Position, f.Kept = 0, 0 }}, SuggestionKeep},
		{"last examined entry", []change{func(f *SuggestionKeepFacts) { f.Position = MaxSuggestionEntriesExamined - 1 }}, SuggestionKeep},
		{"one below the limit", []change{func(f *SuggestionKeepFacts) { f.Kept = MaxSuggestionsPerReport - 1 }}, SuggestionKeep},

		{"negative kept", []change{func(f *SuggestionKeepFacts) { f.Kept = -1 }}, SuggestionInvalidFacts},
		{"kept past the limit", []change{func(f *SuggestionKeepFacts) { f.Kept = MaxSuggestionsPerReport + 1 }}, SuggestionInvalidFacts},
		{"negative position", []change{func(f *SuggestionKeepFacts) { f.Position = -1 }}, SuggestionInvalidFacts},
		{"invalid facts precede the examined cap", []change{func(f *SuggestionKeepFacts) { f.Kept, f.Position = -1, MaxSuggestionEntriesExamined }}, SuggestionInvalidFacts},

		{"past the examined entries", []change{func(f *SuggestionKeepFacts) { f.Position = MaxSuggestionEntriesExamined }}, SuggestionOverLimit},
		{"undecoded entry past the cap", []change{func(f *SuggestionKeepFacts) { *f = SuggestionKeepFacts{Position: MaxSuggestionEntriesExamined + 5} }}, SuggestionOverLimit},

		{"not decoded", []change{func(f *SuggestionKeepFacts) { f.Decoded = false }}, SuggestionMalformedEntry},
		{"malformed precedes identity", []change{func(f *SuggestionKeepFacts) { f.Decoded, f.RecordID = false, "" }}, SuggestionMalformedEntry},

		{"empty record id", []change{func(f *SuggestionKeepFacts) { f.RecordID = "" }}, SuggestionInvalidIdentity},
		{"record id with a dot", []change{func(f *SuggestionKeepFacts) { f.RecordID = "record.1" }}, SuggestionInvalidIdentity},
		{"version id too long", []change{func(f *SuggestionKeepFacts) { f.VersionID = strings.Repeat("v", 129) }}, SuggestionInvalidIdentity},
		{"version id at the bound", []change{func(f *SuggestionKeepFacts) { f.VersionID = strings.Repeat("v", 128) }}, SuggestionKeep},
		{"uppercase hash", []change{func(f *SuggestionKeepFacts) { f.ContentHash = strings.ToUpper(auditHash) }}, SuggestionInvalidIdentity},
		{"hash without prefix", []change{func(f *SuggestionKeepFacts) { f.ContentHash = auditHash[len("sha256:"):] }}, SuggestionInvalidIdentity},
		{"identity changed by redaction", []change{func(f *SuggestionKeepFacts) { f.IdentityChangedByRedaction = true }}, SuggestionInvalidIdentity},
		{"identity precedes the action", []change{func(f *SuggestionKeepFacts) { f.RecordID, f.Action = "", "forget" }}, SuggestionInvalidIdentity},

		{"unknown action", []change{func(f *SuggestionKeepFacts) { f.Action = "forget" }}, SuggestionUnknownAction},
		{"empty action", []change{func(f *SuggestionKeepFacts) { f.Action = "" }}, SuggestionUnknownAction},
		{"uppercase action", []change{func(f *SuggestionKeepFacts) { f.Action = "ARCHIVE" }}, SuggestionUnknownAction},
		{"action precedes the reason", []change{func(f *SuggestionKeepFacts) { f.Action, f.Reason = "forget", "" }}, SuggestionUnknownAction},

		{"empty reason", []change{func(f *SuggestionKeepFacts) { f.Reason = "" }}, SuggestionEmptyReason},
		{"blank reason", []change{func(f *SuggestionKeepFacts) { f.Reason = " \n\t" }}, SuggestionEmptyReason},
		{"reason precedes duplicate", []change{func(f *SuggestionKeepFacts) { f.Reason, f.RepeatsEarlierRecord = "", true }}, SuggestionEmptyReason},

		{"repeated record", []change{func(f *SuggestionKeepFacts) { f.RepeatsEarlierRecord = true }}, SuggestionDuplicate},
		{"duplicate precedes the limit", []change{func(f *SuggestionKeepFacts) { f.RepeatsEarlierRecord, f.Kept = true, MaxSuggestionsPerReport }}, SuggestionDuplicate},

		{"limit reached", []change{func(f *SuggestionKeepFacts) { f.Kept = MaxSuggestionsPerReport }}, SuggestionOverLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideSuggestionKeep(keepFacts(tt.changes...))
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func standingFacts(change ...func(*SuggestionStandingFacts)) SuggestionStandingFacts {
	f := SuggestionStandingFacts{
		RequestProjectID: "p1",
		ReviewFound:      true, Actor: ActorAgent, Decision: ReviewOpinion, Suggestion: SuggestArchive,
		VersionID: "v2", ContentHash: auditHash,
		RecordFound:        true,
		Record:             Record{ID: "r1", ProjectID: "p1", Status: StatusActive, CurrentVersionID: "v2", Version: 4},
		CurrentContentHash: auditHash,
	}
	for _, c := range change {
		c(&f)
	}
	return f
}

func TestDecideSuggestionStanding(t *testing.T) {
	type change = func(*SuggestionStandingFacts)
	tests := []struct {
		name    string
		changes []change
		want    SuggestionKind
	}{
		{"archive on an active record", nil, SuggestionApplies},
		{"archive on a stale record", []change{func(f *SuggestionStandingFacts) { f.Record.Status = StatusNeedsVerification }}, SuggestionApplies},
		{"delete on an active record", []change{func(f *SuggestionStandingFacts) { f.Suggestion = SuggestDelete }}, SuggestionApplies},
		{"delete on an archived record", []change{func(f *SuggestionStandingFacts) { f.Suggestion, f.Record.Status = SuggestDelete, StatusArchived }}, SuggestionApplies},

		{"no request project", []change{func(f *SuggestionStandingFacts) { f.RequestProjectID = "" }}, SuggestionInvalidInput},

		{"missing review", []change{func(f *SuggestionStandingFacts) { f.ReviewFound = false }}, SuggestionNotFound},
		{"system opinion", []change{func(f *SuggestionStandingFacts) { f.Actor = ActorSystem }}, SuggestionNotFound},
		{"human review", []change{func(f *SuggestionStandingFacts) { f.Actor, f.Decision = ActorHuman, ReviewApprove }}, SuggestionNotFound},
		{"agent approval", []change{func(f *SuggestionStandingFacts) { f.Decision = ReviewApprove }}, SuggestionNotFound},
		{"opinion without suggestion", []change{func(f *SuggestionStandingFacts) { f.Suggestion = "" }}, SuggestionNotFound},
		{"unknown suggestion", []change{func(f *SuggestionStandingFacts) { f.Suggestion = "forget" }}, SuggestionNotFound},
		{"missing record", []change{func(f *SuggestionStandingFacts) { f.RecordFound = false }}, SuggestionNotFound},
		{"record of another project", []change{func(f *SuggestionStandingFacts) { f.Record.ProjectID = "p2" }}, SuggestionNotFound},
		{"foreign precedes stale", []change{func(f *SuggestionStandingFacts) { f.Record.ProjectID, f.Record.CurrentVersionID = "p2", "v3" }}, SuggestionNotFound},

		{"newer version", []change{func(f *SuggestionStandingFacts) { f.Record.CurrentVersionID = "v3" }}, SuggestionStale},
		{"other current hash", []change{func(f *SuggestionStandingFacts) { f.CurrentContentHash = auditOther }}, SuggestionStale},
		{"stale precedes deleted", []change{func(f *SuggestionStandingFacts) { f.Record.CurrentVersionID, f.CurrentContentDeleted = "v3", true }}, SuggestionStale},

		{"deleted content", []change{func(f *SuggestionStandingFacts) { f.CurrentContentDeleted = true }}, SuggestionContentDeleted},
		{"deleted delete suggestion", []change{func(f *SuggestionStandingFacts) { f.Suggestion, f.CurrentContentDeleted = SuggestDelete, true }}, SuggestionContentDeleted},
		{"deleted precedes status", []change{func(f *SuggestionStandingFacts) { f.CurrentContentDeleted, f.Record.Status = true, StatusArchived }}, SuggestionContentDeleted},

		{"archived record", []change{func(f *SuggestionStandingFacts) { f.Record.Status = StatusArchived }}, SuggestionStatusMoved},
		{"superseded record", []change{func(f *SuggestionStandingFacts) { f.Record.Status = StatusSuperseded }}, SuggestionStatusMoved},
		{"delete on an unknown status", []change{func(f *SuggestionStandingFacts) { f.Suggestion, f.Record.Status = SuggestDelete, "lost" }}, SuggestionStatusMoved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := standingFacts(tt.changes...)
			got := DecideSuggestionStanding(f)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
			if (got.Kind == SuggestionApplies) != (got.Action == f.Suggestion && got.Action != "") {
				t.Fatalf("action = %q for %s", got.Action, got.Kind)
			}
		})
	}
}

func TestDecideSuggestionConfirmation(t *testing.T) {
	confirm := func(change func(*SuggestionConfirmationFacts)) SuggestionConfirmationFacts {
		f := SuggestionConfirmationFacts{ReviewID: "review-1", Actor: ActorHuman, Standing: standingFacts()}
		if change != nil {
			change(&f)
		}
		return f
	}
	tests := []struct {
		name   string
		change func(*SuggestionConfirmationFacts)
		want   SuggestionKind
	}{
		{"human confirms", nil, SuggestionApplies},
		{"no review id", func(f *SuggestionConfirmationFacts) { f.ReviewID = "" }, SuggestionInvalidInput},
		{"unknown actor", func(f *SuggestionConfirmationFacts) { f.Actor = "operator" }, SuggestionInvalidInput},
		{"agent confirms", func(f *SuggestionConfirmationFacts) { f.Actor = ActorAgent }, SuggestionActorNotAllowed},
		{"system confirms", func(f *SuggestionConfirmationFacts) { f.Actor = ActorSystem }, SuggestionActorNotAllowed},
		{"actor precedes not found", func(f *SuggestionConfirmationFacts) { f.Actor, f.Standing.ReviewFound = ActorAgent, false }, SuggestionActorNotAllowed},
		{"no request project", func(f *SuggestionConfirmationFacts) { f.Standing.RequestProjectID = "" }, SuggestionInvalidInput},
		{"not found", func(f *SuggestionConfirmationFacts) { f.Standing.ReviewFound = false }, SuggestionNotFound},
		{"stale", func(f *SuggestionConfirmationFacts) { f.Standing.Record.CurrentVersionID = "v3" }, SuggestionStale},
		{"status moved", func(f *SuggestionConfirmationFacts) { f.Standing.Record.Status = StatusArchived }, SuggestionStatusMoved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideSuggestionConfirmation(confirm(tt.change))
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}
