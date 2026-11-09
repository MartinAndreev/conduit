package memory

import (
	"testing"
	"time"
)

var created = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func transitionFacts(from, to RecordStatus, actor Actor) RecordTransitionFacts {
	return RecordTransitionFacts{
		Record:             Record{ID: "r1", ProjectID: "p1", Status: from, CurrentVersionID: "v1", Version: 3, CreatedAt: created},
		CurrentContentHash: "c1",
		To:                 to,
		Actor:              actor,
		ExpectedVersion:    3,
	}
}

func TestDecideRecordTransition(t *testing.T) {
	verifiedA := SourceVerification{Path: "a.go", CurrentHash: "new", VerifiedHash: "new"}
	verifiedB := SourceVerification{Path: "b.go", CurrentHash: "same", VerifiedHash: "same"}
	reactivation := func(list ...SourceVerification) RecordTransitionFacts {
		f := transitionFacts(StatusNeedsVerification, StatusActive, ActorHuman)
		f.DeclaredSources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "old"}, {Kind: SourceFile, Path: "b.go", BlobHash: "same"}, {Kind: SourceEvent, Ref: "e1"}}
		f.Verifications = list
		f.NewVersionID, f.ApprovedVersionID = "v2", "v2"
		f.NewVersionContentHash, f.ApprovedContentHash = "c2", "c2"
		f.NewVersionSources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "new"}, {Kind: SourceFile, Path: "b.go", BlobHash: "same"}, {Kind: SourceEvent, Ref: "e1"}}
		return f
	}
	with := func(f RecordTransitionFacts, change func(*RecordTransitionFacts)) RecordTransitionFacts {
		change(&f)
		return f
	}
	approve := func(f *RecordTransitionFacts) { f.ApprovedVersionID, f.ApprovedContentHash = "v1", "c1" }
	reason := func(f *RecordTransitionFacts) { f.Reason = "no longer relevant" }
	replace := func(f *RecordTransitionFacts) { f.ReplacementRecordID, f.ReplacementBecomesActive = "r2", true }
	at := func(age time.Duration) func(*RecordTransitionFacts) {
		return func(f *RecordTransitionFacts) { f.Now = created.Add(age) }
	}
	source := func(kind ApplicabilityKind) func(*RecordTransitionFacts) {
		return func(f *RecordTransitionFacts) { f.SourceApplicability = kind }
	}

	tests := []struct {
		name  string
		facts RecordTransitionFacts
		want  RecordTransitionKind
	}{
		{"approve exact version", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), approve), TransitionAllowed},
		{"reject with reason", with(transitionFacts(StatusProposed, StatusRejected, ActorHuman), reason), TransitionAllowed},
		{"archive stale proposal", with(transitionFacts(StatusProposed, StatusArchived, ActorSystem), at(91*24*time.Hour)), TransitionAllowed},
		{"archive at exactly 90 days", with(transitionFacts(StatusProposed, StatusArchived, ActorSystem), at(proposalRetention)), TransitionAllowed},
		{"hash mismatch needs verification", with(transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem), source(ApplicabilityMismatch)), TransitionAllowed},
		{"verified returns to active", reactivation(verifiedA, verifiedB), TransitionAllowed},
		{"active superseded", with(transitionFacts(StatusActive, StatusSuperseded, ActorHuman), replace), TransitionAllowed},
		{"needs verification superseded", with(transitionFacts(StatusNeedsVerification, StatusSuperseded, ActorHuman), replace), TransitionAllowed},
		{"active archived", with(transitionFacts(StatusActive, StatusArchived, ActorHuman), reason), TransitionAllowed},
		{"needs verification archived", with(transitionFacts(StatusNeedsVerification, StatusArchived, ActorHuman), reason), TransitionAllowed},

		{"empty actor is invalid", transitionFacts(StatusProposed, StatusActive, ""), TransitionInvalidFacts},
		{"unknown from is invalid", transitionFacts("draft", StatusActive, ActorHuman), TransitionInvalidFacts},
		{"unknown to is invalid", transitionFacts(StatusProposed, "draft", ActorHuman), TransitionInvalidFacts},
		{"missing expected version is invalid", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ExpectedVersion = 0 }), TransitionInvalidFacts},
		{"missing record version is invalid", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.Record.Version = 0 }), TransitionInvalidFacts},
		{"retention without clock is invalid", transitionFacts(StatusProposed, StatusArchived, ActorSystem), TransitionInvalidFacts},

		{"stale expected version", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ExpectedVersion = 2 }), TransitionVersionConflict},
		{"newer expected version", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ExpectedVersion = 4 }), TransitionVersionConflict},
		{"conflict precedes terminal refusal", with(transitionFacts(StatusRejected, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ExpectedVersion = 2 }), TransitionVersionConflict},

		{"rejected is terminal", transitionFacts(StatusRejected, StatusActive, ActorHuman), TransitionNotPermitted},
		{"superseded is terminal", transitionFacts(StatusSuperseded, StatusActive, ActorHuman), TransitionNotPermitted},
		{"archived is terminal", transitionFacts(StatusArchived, StatusProposed, ActorHuman), TransitionNotPermitted},
		{"same status", transitionFacts(StatusActive, StatusActive, ActorHuman), TransitionNotPermitted},
		{"active cannot be rejected", transitionFacts(StatusActive, StatusRejected, ActorHuman), TransitionNotPermitted},
		{"proposed cannot need verification", transitionFacts(StatusProposed, StatusNeedsVerification, ActorSystem), TransitionNotPermitted},

		{"agent cannot approve", with(transitionFacts(StatusProposed, StatusActive, ActorAgent), approve), TransitionActorNotAllowed},
		{"system cannot approve", with(transitionFacts(StatusProposed, StatusActive, ActorSystem), approve), TransitionActorNotAllowed},
		{"human cannot expire proposal", with(transitionFacts(StatusProposed, StatusArchived, ActorHuman), at(proposalRetention)), TransitionActorNotAllowed},
		{"human cannot mark stale", with(transitionFacts(StatusActive, StatusNeedsVerification, ActorHuman), source(ApplicabilityMismatch)), TransitionActorNotAllowed},
		{"system cannot archive active", with(transitionFacts(StatusActive, StatusArchived, ActorSystem), reason), TransitionActorNotAllowed},

		{"approval of old version", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ApprovedVersionID, f.ApprovedContentHash = "v0", "c1" }), TransitionApprovalMismatch},
		{"approval of other hash", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ApprovedVersionID, f.ApprovedContentHash = "v1", "c0" }), TransitionApprovalMismatch},
		{"approval naming nothing", transitionFacts(StatusProposed, StatusActive, ActorHuman), TransitionApprovalMismatch},

		{"reject without reason", transitionFacts(StatusProposed, StatusRejected, ActorHuman), TransitionReasonRequired},
		{"archive with blank reason", with(transitionFacts(StatusActive, StatusArchived, ActorHuman), func(f *RecordTransitionFacts) { f.Reason = "  " }), TransitionReasonRequired},

		{"one second before 90 days", with(transitionFacts(StatusProposed, StatusArchived, ActorSystem), at(proposalRetention-time.Second)), TransitionRetentionNotElapsed},
		{"decided proposal is not expired", with(transitionFacts(StatusProposed, StatusArchived, ActorSystem), func(f *RecordTransitionFacts) {
			f.Now = created.Add(proposalRetention)
			f.HumanDecisionRecorded = true
		}), TransitionDecisionRecorded},

		{"matching source does not go stale", with(transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem), source(ApplicabilityApplicable)), TransitionSourceNotMismatched},
		{"no file source does not go stale", with(transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem), source(ApplicabilityUnknown)), TransitionSourceNotMismatched},
		{"missing applicability does not go stale", transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem), TransitionSourceNotMismatched},
		{"unavailable source needs verification", with(transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem), source(ApplicabilitySourceUnavailable)), TransitionAllowed},

		{"no verification", reactivation(), TransitionVerificationMissing},
		{"verified against stale hash", reactivation(SourceVerification{Path: "a.go", CurrentHash: "new", VerifiedHash: "older"}, verifiedB), TransitionVerificationMissing},
		{"empty hashes are not a verification", reactivation(SourceVerification{Path: "a.go"}, verifiedB), TransitionVerificationMissing},
		{"declared source left unverified", reactivation(verifiedA), TransitionVerificationMissing},
		{"duplicate verification", reactivation(verifiedA, verifiedA), TransitionVerificationMissing},
		{"unrelated verification", reactivation(verifiedA, SourceVerification{Path: "c.go", CurrentHash: "x", VerifiedHash: "x"}), TransitionVerificationMissing},
		{"extra verification", reactivation(verifiedA, verifiedB, SourceVerification{Path: "c.go", CurrentHash: "x", VerifiedHash: "x"}), TransitionVerificationMissing},
		{"empty declared set", with(reactivation(verifiedA), func(f *RecordTransitionFacts) { f.DeclaredSources = nil }), TransitionVerificationMissing},
		{"declared set without file sources", with(reactivation(), func(f *RecordTransitionFacts) { f.DeclaredSources = []RecordSource{{Kind: SourceEvent, Ref: "e1"}} }), TransitionVerificationMissing},
		{"source back at declared hash reactivates", with(reactivation(SourceVerification{Path: "a.go", CurrentHash: "old", VerifiedHash: "old"}, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources[0].BlobHash = "old" }), TransitionAllowed},
		{"reactivation without new version", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionID, f.ApprovedVersionID = "", "" }), TransitionApprovalMismatch},
		{"reactivation reusing current version", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionID, f.ApprovedVersionID = "v1", "v1" }), TransitionApprovalMismatch},
		{"approval names old version on reactivation", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedVersionID = "v1" }), TransitionApprovalMismatch},
		{"approval names wrong content hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedContentHash = "c0" }), TransitionApprovalMismatch},
		{"approval names old content hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedContentHash = "c1" }), TransitionApprovalMismatch},
		{"empty approval and new content hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedContentHash, f.NewVersionContentHash = "", "" }), TransitionApprovalMismatch},
		{"new version retains old source hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources[0].BlobHash = "old" }), TransitionApprovalMismatch},
		{"new version with empty source hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources[1].BlobHash = "" }), TransitionApprovalMismatch},
		{"new version misses a path", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources = f.NewVersionSources[:1] }), TransitionApprovalMismatch},
		{"new version has no sources", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources = nil }), TransitionApprovalMismatch},
		{"new version adds a path", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.NewVersionSources = append(f.NewVersionSources, RecordSource{Kind: SourceFile, Path: "c.go", BlobHash: "x"})
		}), TransitionApprovalMismatch},
		{"new version repeats a path", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.NewVersionSources = append(f.NewVersionSources, RecordSource{Kind: SourceFile, Path: "a.go", BlobHash: "new"})
		}), TransitionApprovalMismatch},
		{"declared path repeated", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.DeclaredSources = append(f.DeclaredSources, RecordSource{Kind: SourceFile, Path: "a.go", BlobHash: "old"})
		}), TransitionVerificationMissing},
		{"declared path empty", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.DeclaredSources = append(f.DeclaredSources, RecordSource{Kind: SourceFile, BlobHash: "old"})
		}), TransitionVerificationMissing},
		{"verification precedes new version check", with(reactivation(verifiedA), func(f *RecordTransitionFacts) { f.NewVersionSources = nil }), TransitionVerificationMissing},
		{"verification precedes approval", with(reactivation(verifiedA), func(f *RecordTransitionFacts) { f.ApprovedVersionID = "v1" }), TransitionVerificationMissing},

		{"no replacement", transitionFacts(StatusActive, StatusSuperseded, ActorHuman), TransitionReplacementMissing},
		{"replacement not active", with(transitionFacts(StatusActive, StatusSuperseded, ActorHuman), func(f *RecordTransitionFacts) { f.ReplacementRecordID = "r2" }), TransitionReplacementMissing},
		{"self replacement is an overwrite", with(transitionFacts(StatusActive, StatusSuperseded, ActorHuman), func(f *RecordTransitionFacts) { f.ReplacementRecordID, f.ReplacementBecomesActive = "r1", true }), TransitionReplacementMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideRecordTransition(tt.facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestTerminalStatusesHaveNoOutgoingTransition(t *testing.T) {
	for from, rule := range transitionTable {
		if from[0].Terminal() {
			t.Fatalf("terminal status has transition %v by %s", from, rule.actor)
		}
	}
}
