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

		{"dismissed", []change{func(f *SuggestionStandingFacts) { f.Dismissed = true }}, SuggestionDismissed},
		{"dismissed delete", []change{func(f *SuggestionStandingFacts) { f.Suggestion, f.Dismissed = SuggestDelete, true }}, SuggestionDismissed},
		{"stale precedes dismissed", []change{func(f *SuggestionStandingFacts) { f.Record.CurrentVersionID, f.Dismissed = "v3", true }}, SuggestionStale},
		{"dismissed precedes deleted", []change{func(f *SuggestionStandingFacts) { f.Dismissed, f.CurrentContentDeleted = true, true }}, SuggestionDismissed},
		{"dismissed precedes status", []change{func(f *SuggestionStandingFacts) { f.Dismissed, f.Record.Status = true, StatusArchived }}, SuggestionDismissed},

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
		{"dismissed", func(f *SuggestionConfirmationFacts) { f.Standing.Dismissed = true }, SuggestionDismissed},
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

func TestDecideSuggestionDismissal(t *testing.T) {
	type change = func(*SuggestionDismissalFacts)
	dismissal := func(changes ...change) SuggestionDismissalFacts {
		f := SuggestionDismissalFacts{ReviewID: "review-1", Actor: ActorHuman, Reviewer: "martin", ExpectedVersion: 4, Reason: "still needed", Standing: standingFacts()}
		for _, c := range changes {
			c(&f)
		}
		return f
	}
	tests := []struct {
		name    string
		changes []change
		want    SuggestionDismissalKind
	}{
		{"human dismisses archive", nil, SuggestionDismiss},
		{"human dismisses delete", []change{func(f *SuggestionDismissalFacts) { f.Standing.Suggestion = SuggestDelete }}, SuggestionDismiss},
		{"no reason", []change{func(f *SuggestionDismissalFacts) { f.Reason = "" }}, SuggestionDismiss},
		{"reason at the bound", []change{func(f *SuggestionDismissalFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes) }}, SuggestionDismiss},

		{"no review id", []change{func(f *SuggestionDismissalFacts) { f.ReviewID = "" }}, SuggestionDismissalInvalidFacts},
		{"unknown actor", []change{func(f *SuggestionDismissalFacts) { f.Actor = "operator" }}, SuggestionDismissalInvalidFacts},
		{"no reviewer", []change{func(f *SuggestionDismissalFacts) { f.Reviewer = "" }}, SuggestionDismissalInvalidFacts},
		{"reviewer one byte over", []change{func(f *SuggestionDismissalFacts) { f.Reviewer = strings.Repeat("ж", MaxIdentityBytes/2) + "x" }}, SuggestionDismissalInvalidFacts},
		{"reviewer at the bound", []change{func(f *SuggestionDismissalFacts) { f.Reviewer = strings.Repeat("ж", MaxIdentityBytes/2) }}, SuggestionDismiss},
		{"no expected version", []change{func(f *SuggestionDismissalFacts) { f.ExpectedVersion = 0 }}, SuggestionDismissalInvalidFacts},
		{"no request project", []change{func(f *SuggestionDismissalFacts) { f.Standing.RequestProjectID = "" }}, SuggestionDismissalInvalidFacts},

		{"agent dismisses", []change{func(f *SuggestionDismissalFacts) { f.Actor = ActorAgent }}, SuggestionDismissalActorNotAllowed},
		{"system dismisses", []change{func(f *SuggestionDismissalFacts) { f.Actor = ActorSystem }}, SuggestionDismissalActorNotAllowed},
		{"actor precedes not found", []change{func(f *SuggestionDismissalFacts) { f.Actor, f.Standing.ReviewFound = ActorAgent, false }}, SuggestionDismissalActorNotAllowed},
		{"actor precedes a project check", []change{func(f *SuggestionDismissalFacts) { f.Actor, f.Standing.RequestProjectID = ActorSystem, "" }}, SuggestionDismissalActorNotAllowed},

		{"missing review", []change{func(f *SuggestionDismissalFacts) { f.Standing.ReviewFound = false }}, SuggestionDismissalNotFound},
		{"a dismissal is not a suggestion", []change{func(f *SuggestionDismissalFacts) {
			f.Standing.Actor, f.Standing.Decision = ActorHuman, ReviewDismiss
		}}, SuggestionDismissalNotFound},
		{"record of another project", []change{func(f *SuggestionDismissalFacts) { f.Standing.Record.ProjectID = "p2" }}, SuggestionDismissalNotFound},

		{"newer version", []change{func(f *SuggestionDismissalFacts) { f.Standing.Record.CurrentVersionID = "v3" }}, SuggestionDismissalStale},
		{"already dismissed", []change{func(f *SuggestionDismissalFacts) { f.Standing.Dismissed = true }}, SuggestionAlreadyDismissed},
		{"deleted content", []change{func(f *SuggestionDismissalFacts) { f.Standing.CurrentContentDeleted = true }}, SuggestionDismissalContentDeleted},
		{"status moved", []change{func(f *SuggestionDismissalFacts) { f.Standing.Record.Status = StatusArchived }}, SuggestionDismissalStatusMoved},
		{"status precedes the version", []change{func(f *SuggestionDismissalFacts) { f.Standing.Record.Status, f.ExpectedVersion = StatusArchived, 3 }}, SuggestionDismissalStatusMoved},

		{"other expected version", []change{func(f *SuggestionDismissalFacts) { f.ExpectedVersion = 3 }}, SuggestionDismissalVersionConflict},
		{"version precedes a long reason", []change{func(f *SuggestionDismissalFacts) {
			f.ExpectedVersion, f.Reason = 5, strings.Repeat("r", MaxTextFieldBytes+1)
		}}, SuggestionDismissalVersionConflict},

		{"reason one byte over", []change{func(f *SuggestionDismissalFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes+1) }}, SuggestionDismissalReasonTooLong},
		{"reason measured in bytes", []change{func(f *SuggestionDismissalFacts) { f.Reason = strings.Repeat("ж", MaxTextFieldBytes/2) + "r" }}, SuggestionDismissalReasonTooLong},
	}
	seen := map[SuggestionDismissalKind]bool{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := dismissal(tt.changes...)
			got := DecideSuggestionDismissal(f)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
			if (got.Kind == SuggestionDismiss) != (got.Action == f.Standing.Suggestion && got.Action != "") {
				t.Fatalf("action = %q for %s", got.Action, got.Kind)
			}
		})
		seen[tt.want] = true
	}
	for _, kind := range SuggestionDismissalKinds() {
		if !seen[kind] {
			t.Fatalf("kind %s is not tested", kind)
		}
	}
	if len(seen) != len(SuggestionDismissalKinds()) {
		t.Fatalf("tested kinds %v", seen)
	}
}

// Every standing kind but applies maps to a dismissal kind.
func TestDismissalKindsCoverEveryStandingRefusal(t *testing.T) {
	for _, kind := range []SuggestionKind{SuggestionInvalidInput, SuggestionNotFound, SuggestionStale, SuggestionDismissed, SuggestionContentDeleted, SuggestionStatusMoved} {
		if _, ok := dismissalKinds[kind]; !ok {
			t.Fatalf("standing %s has no dismissal kind", kind)
		}
	}
	if len(dismissalKinds) != 6 {
		t.Fatalf("dismissal kinds = %d", len(dismissalKinds))
	}
}

// standingVariants are standings that each lead the standing rule to a
// different kind, all of the same request project.
func standingVariants() []SuggestionStandingFacts {
	return []SuggestionStandingFacts{
		{RequestProjectID: "p1"},
		standingFacts(),
		standingFacts(func(f *SuggestionStandingFacts) { f.Record.ProjectID = "p2" }),
		standingFacts(func(f *SuggestionStandingFacts) { f.Record.CurrentVersionID = "v3" }),
		standingFacts(func(f *SuggestionStandingFacts) { f.Dismissed = true }),
		standingFacts(func(f *SuggestionStandingFacts) { f.CurrentContentDeleted = true }),
		standingFacts(func(f *SuggestionStandingFacts) { f.Record.Status = StatusArchived }),
	}
}

// The procedures refuse invalid_facts and actor_not_allowed before reading
// anything, deciding from the request facts and an empty standing of the
// request project. That is sound only because those two results never depend
// on the stored standing: for any standing they are the same decision.
func TestConfirmationActorAndInvalidFactsIgnoreTheStanding(t *testing.T) {
	for _, request := range []SuggestionConfirmationFacts{
		{ReviewID: "review-1", Actor: ActorAgent},
		{ReviewID: "review-1", Actor: ActorSystem},
		{ReviewID: "review-1", Actor: "operator"},
		{Actor: ActorHuman},
		{Actor: ActorAgent},
	} {
		request.Standing = SuggestionStandingFacts{RequestProjectID: "p1"}
		want := DecideSuggestionConfirmation(request)
		if want.Kind != SuggestionInvalidInput && want.Kind != SuggestionActorNotAllowed {
			t.Fatalf("%+v decided %s", request, want.Kind)
		}
		for _, standing := range standingVariants() {
			request.Standing = standing
			if got := DecideSuggestionConfirmation(request); got != want {
				t.Fatalf("%+v = %+v, want %+v", request, got, want)
			}
		}
	}
	// A human's request facts never decide those kinds unless the project
	// itself is missing, which is a request fact too.
	for _, standing := range standingVariants() {
		if got := DecideSuggestionConfirmation(SuggestionConfirmationFacts{ReviewID: "review-1", Actor: ActorHuman, Standing: standing}); got.Kind == SuggestionInvalidInput || got.Kind == SuggestionActorNotAllowed {
			t.Fatalf("human with %+v = %+v", standing, got)
		}
	}
}

func TestDismissalActorAndInvalidFactsIgnoreTheStanding(t *testing.T) {
	valid := SuggestionDismissalFacts{ReviewID: "review-1", Actor: ActorHuman, Reviewer: "martin", ExpectedVersion: 4}
	for _, change := range []func(*SuggestionDismissalFacts){
		func(f *SuggestionDismissalFacts) { f.Actor = ActorAgent },
		func(f *SuggestionDismissalFacts) { f.Actor = ActorSystem },
		func(f *SuggestionDismissalFacts) { f.Actor = "operator" },
		func(f *SuggestionDismissalFacts) { f.ReviewID = "" },
		func(f *SuggestionDismissalFacts) { f.Reviewer = "" },
		func(f *SuggestionDismissalFacts) { f.ExpectedVersion = 0 },
		func(f *SuggestionDismissalFacts) { f.Actor, f.ExpectedVersion = ActorAgent, 0 },
	} {
		request := valid
		change(&request)
		request.Standing = SuggestionStandingFacts{RequestProjectID: "p1"}
		want := DecideSuggestionDismissal(request)
		if want.Kind != SuggestionDismissalInvalidFacts && want.Kind != SuggestionDismissalActorNotAllowed {
			t.Fatalf("%+v decided %s", request, want.Kind)
		}
		for _, standing := range standingVariants() {
			request.Standing = standing
			if got := DecideSuggestionDismissal(request); got != want {
				t.Fatalf("%+v = %+v, want %+v", request, got, want)
			}
		}
	}
	for _, standing := range standingVariants() {
		request := valid
		request.Standing = standing
		if got := DecideSuggestionDismissal(request); got.Kind == SuggestionDismissalInvalidFacts || got.Kind == SuggestionDismissalActorNotAllowed {
			t.Fatalf("human with %+v = %+v", standing, got)
		}
	}
}
