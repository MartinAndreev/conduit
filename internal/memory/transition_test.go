package memory

import (
	"strings"
	"testing"
	"time"
)

var created = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func transitionFacts(from, to RecordStatus, actor Actor) RecordTransitionFacts {
	return RecordTransitionFacts{
		RequestProjectID:   "p1",
		RecordFound:        true,
		Record:             Record{ID: "r1", ProjectID: "p1", Status: from, CurrentVersionID: "v1", Version: 3, CreatedAt: created},
		CurrentContentHash: "c1",
		To:                 to,
		Actor:              actor,
		ExpectedVersion:    3,
		Reviewer:           "martin",
	}
}

func TestDecideRecordTransition(t *testing.T) {
	verifiedA := SourceVerification{Path: "a.go", CurrentHash: "c2a6b03f190dfb2b4aa91f8af8d477a9bc3401dc", VerifiedHash: "c2a6b03f190dfb2b4aa91f8af8d477a9bc3401dc"}
	verifiedB := SourceVerification{Path: "b.go", CurrentHash: "ff3390557335ba88d37755e41514beb03bc499ec", VerifiedHash: "ff3390557335ba88d37755e41514beb03bc499ec"}
	reactivation := func(list ...SourceVerification) RecordTransitionFacts {
		f := transitionFacts(StatusNeedsVerification, StatusActive, ActorHuman)
		f.DeclaredSources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "c00dbbc9dadfbe1e232e93a729dd4752fade0abf"}, {Kind: SourceFile, Path: "b.go", BlobHash: "ff3390557335ba88d37755e41514beb03bc499ec"}, {Kind: SourceEvent, Ref: "e1"}}
		f.Verifications = list
		f.NewVersionID, f.ApprovedVersionID = "v2", "v2"
		f.NewVersionContentHash, f.ApprovedContentHash = "c2", "c2"
		f.NewVersionSources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "c2a6b03f190dfb2b4aa91f8af8d477a9bc3401dc"}, {Kind: SourceFile, Path: "b.go", BlobHash: "ff3390557335ba88d37755e41514beb03bc499ec"}, {Kind: SourceEvent, Ref: "e1"}}
		return f
	}
	with := func(f RecordTransitionFacts, changes ...func(*RecordTransitionFacts)) RecordTransitionFacts {
		for _, change := range changes {
			change(&f)
		}
		return f
	}
	deleted := func(f *RecordTransitionFacts) { f.CurrentContentDeleted = true }
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
		{"unknown actor is invalid", transitionFacts(StatusProposed, StatusActive, "operator"), TransitionInvalidFacts},
		{"missing request project is invalid", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.RequestProjectID = "" }), TransitionInvalidFacts},
		{"invalid request precedes not found", with(transitionFacts(StatusProposed, StatusActive, ""), func(f *RecordTransitionFacts) { f.RecordFound = false }), TransitionInvalidFacts},

		{"missing record", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.RecordFound, f.Record = false, Record{} }), TransitionNotFound},
		{"record of another project", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.RequestProjectID = "p2" }), TransitionNotFound},
		{"not found precedes conflict", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.RequestProjectID, f.ExpectedVersion = "p2", 2 }), TransitionNotFound},

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

		{"approval of deleted content", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), approve, deleted), TransitionContentDeleted},
		{"rejection of deleted content", with(transitionFacts(StatusProposed, StatusRejected, ActorHuman), reason, deleted), TransitionContentDeleted},
		{"human archive of deleted content", with(transitionFacts(StatusActive, StatusArchived, ActorHuman), reason, deleted), TransitionContentDeleted},
		{"human archive of deleted content needing verification", with(transitionFacts(StatusNeedsVerification, StatusArchived, ActorHuman), reason, deleted), TransitionContentDeleted},
		{"reactivation of deleted content", with(reactivation(verifiedA, verifiedB), deleted), TransitionContentDeleted},
		{"deleted content precedes missing reason", with(transitionFacts(StatusProposed, StatusRejected, ActorHuman), deleted), TransitionContentDeleted},
		{"actor precedes deleted content", with(transitionFacts(StatusProposed, StatusActive, ActorAgent), approve, deleted), TransitionActorNotAllowed},
		{"approval without reviewer", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), approve, func(f *RecordTransitionFacts) { f.Reviewer = "" }), TransitionInvalidFacts},
		{"approval reviewer at the bound", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), approve, func(f *RecordTransitionFacts) { f.Reviewer = strings.Repeat("ж", MaxIdentityBytes/2) }), TransitionAllowed},
		{"approval reviewer one byte over", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), approve, func(f *RecordTransitionFacts) { f.Reviewer = strings.Repeat("ж", MaxIdentityBytes/2) + "x" }), TransitionInvalidFacts},
		{"rejection reviewer one byte over", with(transitionFacts(StatusProposed, StatusRejected, ActorHuman), reason, func(f *RecordTransitionFacts) { f.Reviewer = strings.Repeat("r", MaxIdentityBytes+1) }), TransitionInvalidFacts},
		{"archive reviewer one byte over", with(transitionFacts(StatusActive, StatusArchived, ActorHuman), reason, func(f *RecordTransitionFacts) { f.Reviewer = strings.Repeat("r", MaxIdentityBytes+1) }), TransitionInvalidFacts},
		{"reactivation reviewer one byte over", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.Reviewer = strings.Repeat("r", MaxIdentityBytes+1) }), TransitionInvalidFacts},
		{"reviewer precedes deleted content", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), approve, deleted, func(f *RecordTransitionFacts) { f.Reviewer = "" }), TransitionInvalidFacts},
		{"actor precedes reviewer", with(transitionFacts(StatusProposed, StatusActive, ActorAgent), approve, func(f *RecordTransitionFacts) { f.Reviewer = "" }), TransitionActorNotAllowed},
		{"system archive needs no reviewer", with(transitionFacts(StatusProposed, StatusArchived, ActorSystem), at(proposalRetention), func(f *RecordTransitionFacts) { f.Reviewer = "" }), TransitionAllowed},
		{"supersede writes no review on the old record", with(transitionFacts(StatusActive, StatusSuperseded, ActorHuman), replace, func(f *RecordTransitionFacts) { f.Reviewer = "" }), TransitionAllowed},
		{"deleted proposal expires", with(transitionFacts(StatusProposed, StatusArchived, ActorSystem), at(proposalRetention), deleted), TransitionAllowed},
		{"deleted record goes stale", with(transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem), source(ApplicabilityMismatch), deleted), TransitionAllowed},
		{"deleted record is superseded", with(transitionFacts(StatusActive, StatusSuperseded, ActorHuman), replace, deleted), TransitionAllowed},

		{"approval of old version", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ApprovedVersionID, f.ApprovedContentHash = "v0", "c1" }), TransitionApprovalMismatch},
		{"approval of other hash", with(transitionFacts(StatusProposed, StatusActive, ActorHuman), func(f *RecordTransitionFacts) { f.ApprovedVersionID, f.ApprovedContentHash = "v1", "c0" }), TransitionApprovalMismatch},
		{"approval naming nothing", transitionFacts(StatusProposed, StatusActive, ActorHuman), TransitionApprovalMismatch},

		{"reject without reason", transitionFacts(StatusProposed, StatusRejected, ActorHuman), TransitionReasonRequired},
		{"archive with blank reason", with(transitionFacts(StatusActive, StatusArchived, ActorHuman), func(f *RecordTransitionFacts) { f.Reason = "  " }), TransitionReasonRequired},
		{"reject with a reason at the bound", with(transitionFacts(StatusProposed, StatusRejected, ActorHuman), func(f *RecordTransitionFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes) }), TransitionAllowed},
		{"reject with a reason one byte over", with(transitionFacts(StatusProposed, StatusRejected, ActorHuman), func(f *RecordTransitionFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes+1) }), TransitionReasonTooLong},
		{"archive with a reason over in bytes", with(transitionFacts(StatusNeedsVerification, StatusArchived, ActorHuman), func(f *RecordTransitionFacts) {
			f.Reason = strings.Repeat("ж", MaxTextFieldBytes/2) + "r"
		}), TransitionReasonTooLong},
		{"deleted content precedes a long reason", with(transitionFacts(StatusProposed, StatusRejected, ActorHuman), deleted, func(f *RecordTransitionFacts) { f.Reason = strings.Repeat("r", MaxTextFieldBytes+1) }), TransitionContentDeleted},

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
		{"current hash is not a blob hash", reactivation(SourceVerification{Path: "a.go", CurrentHash: "password=hunter2", VerifiedHash: "password=hunter2"}, verifiedB), TransitionVerificationMissing},
		{"uppercase current hash", reactivation(SourceVerification{Path: "a.go", CurrentHash: strings.Repeat("A", 40), VerifiedHash: strings.Repeat("A", 40)}, verifiedB), TransitionVerificationMissing},
		{"verified against stale hash", reactivation(SourceVerification{Path: "a.go", CurrentHash: "c2a6b03f190dfb2b4aa91f8af8d477a9bc3401dc", VerifiedHash: "6fa5fbbc16c8f82ea478e5b933b345e46dd285ea"}, verifiedB), TransitionVerificationMissing},
		{"empty hashes are not a verification", reactivation(SourceVerification{Path: "a.go"}, verifiedB), TransitionVerificationMissing},
		{"declared source left unverified", reactivation(verifiedA), TransitionVerificationMissing},
		{"duplicate verification", reactivation(verifiedA, verifiedA), TransitionVerificationMissing},
		{"unrelated verification", reactivation(verifiedA, SourceVerification{Path: "c.go", CurrentHash: "11f6ad8ec52a2984abaafd7c3b516503785c2072", VerifiedHash: "11f6ad8ec52a2984abaafd7c3b516503785c2072"}), TransitionVerificationMissing},
		{"extra verification", reactivation(verifiedA, verifiedB, SourceVerification{Path: "c.go", CurrentHash: "11f6ad8ec52a2984abaafd7c3b516503785c2072", VerifiedHash: "11f6ad8ec52a2984abaafd7c3b516503785c2072"}), TransitionVerificationMissing},
		{"empty declared set", with(reactivation(verifiedA), func(f *RecordTransitionFacts) { f.DeclaredSources = nil }), TransitionVerificationMissing},
		{"declared set without file sources", with(reactivation(), func(f *RecordTransitionFacts) { f.DeclaredSources = []RecordSource{{Kind: SourceEvent, Ref: "e1"}} }), TransitionVerificationMissing},
		{"source back at declared hash reactivates", with(reactivation(SourceVerification{Path: "a.go", CurrentHash: "c00dbbc9dadfbe1e232e93a729dd4752fade0abf", VerifiedHash: "c00dbbc9dadfbe1e232e93a729dd4752fade0abf"}, verifiedB), func(f *RecordTransitionFacts) {
			f.NewVersionSources[0].BlobHash = "c00dbbc9dadfbe1e232e93a729dd4752fade0abf"
		}), TransitionAllowed},
		{"reactivation without new version", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionID, f.ApprovedVersionID = "", "" }), TransitionApprovalMismatch},
		{"reactivation reusing current version", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionID, f.ApprovedVersionID = "v1", "v1" }), TransitionApprovalMismatch},
		{"approval names old version on reactivation", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedVersionID = "v1" }), TransitionApprovalMismatch},
		{"approval names wrong content hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedContentHash = "c0" }), TransitionApprovalMismatch},
		{"approval names old content hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedContentHash = "c1" }), TransitionApprovalMismatch},
		{"empty approval and new content hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.ApprovedContentHash, f.NewVersionContentHash = "", "" }), TransitionApprovalMismatch},
		{"new version retains old source hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.NewVersionSources[0].BlobHash = "c00dbbc9dadfbe1e232e93a729dd4752fade0abf"
		}), TransitionApprovalMismatch},
		{"new version with empty source hash", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources[1].BlobHash = "" }), TransitionApprovalMismatch},
		{"new version misses a path", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources = f.NewVersionSources[:1] }), TransitionApprovalMismatch},
		{"new version has no sources", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) { f.NewVersionSources = nil }), TransitionApprovalMismatch},
		{"new version adds a path", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.NewVersionSources = append(f.NewVersionSources, RecordSource{Kind: SourceFile, Path: "c.go", BlobHash: "11f6ad8ec52a2984abaafd7c3b516503785c2072"})
		}), TransitionApprovalMismatch},
		{"new version repeats a path", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.NewVersionSources = append(f.NewVersionSources, RecordSource{Kind: SourceFile, Path: "a.go", BlobHash: "c2a6b03f190dfb2b4aa91f8af8d477a9bc3401dc"})
		}), TransitionApprovalMismatch},
		{"declared path repeated", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.DeclaredSources = append(f.DeclaredSources, RecordSource{Kind: SourceFile, Path: "a.go", BlobHash: "c00dbbc9dadfbe1e232e93a729dd4752fade0abf"})
		}), TransitionVerificationMissing},
		{"declared path empty", with(reactivation(verifiedA, verifiedB), func(f *RecordTransitionFacts) {
			f.DeclaredSources = append(f.DeclaredSources, RecordSource{Kind: SourceFile, BlobHash: "c00dbbc9dadfbe1e232e93a729dd4752fade0abf"})
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

func TestProposalRetentionCutoff(t *testing.T) {
	now := created.Add(proposalRetention)
	if got := ProposalRetentionCutoff(now); !got.Equal(created) {
		t.Fatalf("cutoff = %v", got)
	}
	f := transitionFacts(StatusProposed, StatusArchived, ActorSystem)
	f.Now = now
	f.Record.CreatedAt = ProposalRetentionCutoff(now)
	if got := DecideRecordTransition(f); got.Kind != TransitionAllowed {
		t.Fatalf("proposal created at the cutoff = %+v", got)
	}
}

// The precheck reports exactly DecideRecordTransition's refusals before the
// precondition, and allows whatever reaches the precondition.
func TestPrecheckRecordTransition(t *testing.T) {
	stale := transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem)
	stale.ExpectedVersion = 2
	deleted := transitionFacts(StatusNeedsVerification, StatusActive, ActorHuman)
	deleted.CurrentContentDeleted = true
	foreign := transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem)
	foreign.RequestProjectID = "p2"
	for _, f := range []RecordTransitionFacts{
		stale, deleted, foreign,
		transitionFacts(StatusActive, StatusNeedsVerification, ActorHuman),
		transitionFacts(StatusArchived, StatusActive, ActorHuman),
		transitionFacts(StatusProposed, StatusActive, ""),
	} {
		want := DecideRecordTransition(f)
		if got := PrecheckRecordTransition(f); got != want || got.Kind == TransitionAllowed {
			t.Fatalf("precheck %+v, decision %+v", got, want)
		}
	}
	// Precondition facts are not consulted: no applicability, no verification.
	for _, f := range []RecordTransitionFacts{
		transitionFacts(StatusActive, StatusNeedsVerification, ActorSystem),
		transitionFacts(StatusNeedsVerification, StatusActive, ActorHuman),
	} {
		if got := PrecheckRecordTransition(f); got.Kind != TransitionAllowed || got.Reason == "" {
			t.Fatalf("precheck = %+v", got)
		}
		if DecideRecordTransition(f).Kind == TransitionAllowed {
			t.Fatal("precondition was not evaluated")
		}
	}
}
