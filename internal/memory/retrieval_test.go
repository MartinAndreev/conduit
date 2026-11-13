package memory

import (
	"slices"
	"testing"
)

func retrievalFacts(change func(*RetrievalFacts)) RetrievalFacts {
	f := RetrievalFacts{
		TargetProjectID:  "p1",
		CurrentAttemptID: "a1",
		RecordFound:      true,
		Record:           Record{ID: "r1", ProjectID: "p1", Status: StatusActive, CurrentVersionID: "v1"},
		Version: RecordVersion{ID: "v1", RecordID: "r1", Trust: TrustConduitObserved, Scope: Scope{
			ProjectID: "p1", Visibility: VisibilityProject, Origin: OriginAttempt, RunID: "run", AttemptID: "a0", BaseCommit: "commit", AcceptedResultRef: "result",
		}},
		ExactVersionApproved: true,
		Applicability:        ApplicabilityApplicable,
	}
	if change != nil {
		change(&f)
	}
	return f
}

func TestDecideRetrievalEligibility(t *testing.T) {
	human := func(f *RetrievalFacts) {
		f.Version.Trust = TrustHumanApproved
		f.Version.Scope = Scope{ProjectID: "p1", Visibility: VisibilityProject, Origin: OriginHuman}
	}
	tests := []struct {
		name       string
		change     func(*RetrievalFacts)
		want       RetrievalKind
		wantLabels []RetrievalLabel
	}{
		{"active applicable", nil, RetrievalEligible, nil},
		{"human approved version", human, RetrievalEligible, nil},
		{"attempt scope in own attempt", func(f *RetrievalFacts) {
			f.Version.Scope.Visibility, f.Version.Scope.AttemptID, f.Version.Scope.AcceptedResultRef = VisibilityAttempt, "a1", ""
		}, RetrievalEligible, nil},
		{"needs verification carries label", func(f *RetrievalFacts) { f.Record.Status = StatusNeedsVerification }, RetrievalEligible, []RetrievalLabel{LabelNeedsVerification}},
		{"human approved without file source", func(f *RetrievalFacts) {
			human(f)
			f.Applicability = ApplicabilityUnknown
		}, RetrievalEligible, []RetrievalLabel{LabelApplicabilityUnknown}},
		{"both labels", func(f *RetrievalFacts) {
			human(f)
			f.Record.Status = StatusNeedsVerification
			f.Applicability = ApplicabilityUnknown
		}, RetrievalEligible, []RetrievalLabel{LabelNeedsVerification, LabelApplicabilityUnknown}},

		{"missing target project", func(f *RetrievalFacts) { f.TargetProjectID = "" }, RetrievalInvalidFacts, nil},
		{"unknown status", func(f *RetrievalFacts) { f.Record.Status = "draft" }, RetrievalInvalidFacts, nil},
		{"unknown trust", func(f *RetrievalFacts) { f.Version.Trust = "score" }, RetrievalInvalidFacts, nil},
		{"unknown visibility", func(f *RetrievalFacts) { f.Version.Scope.Visibility = "" }, RetrievalInvalidFacts, nil},
		{"unknown origin", func(f *RetrievalFacts) { f.Version.Scope.Origin = "" }, RetrievalInvalidFacts, nil},
		{"missing applicability", func(f *RetrievalFacts) { f.Applicability = "" }, RetrievalInvalidFacts, nil},
		{"invalid applicability", func(f *RetrievalFacts) { f.Applicability = ApplicabilityInvalidFacts }, RetrievalInvalidFacts, nil},

		{"attempt origin without run", func(f *RetrievalFacts) { f.Version.Scope.RunID = "" }, RetrievalInvalidFacts, nil},
		{"attempt origin without attempt", func(f *RetrievalFacts) { f.Version.Scope.AttemptID = "" }, RetrievalInvalidFacts, nil},
		{"attempt origin without base commit", func(f *RetrievalFacts) { f.Version.Scope.BaseCommit = "" }, RetrievalInvalidFacts, nil},
		{"human origin with run", func(f *RetrievalFacts) {
			human(f)
			f.Version.Scope.RunID = "run"
		}, RetrievalInvalidFacts, nil},
		{"human origin with attempt", func(f *RetrievalFacts) {
			human(f)
			f.Version.Scope.AttemptID = "a1"
		}, RetrievalInvalidFacts, nil},
		{"absent and not from index", func(f *RetrievalFacts) { *f = RetrievalFacts{TargetProjectID: "p1"} }, RetrievalInvalidFacts, nil},
		{"index hit with no local record", func(f *RetrievalFacts) { *f = RetrievalFacts{TargetProjectID: "p1", FromIndex: true} }, RetrievalStaleIndexResult, nil},

		{"record in another project", func(f *RetrievalFacts) { f.Record.ProjectID = "p2" }, RetrievalForeignProject, nil},
		{"version scope in another project", func(f *RetrievalFacts) { f.Version.Scope.ProjectID = "p2" }, RetrievalForeignProject, nil},

		{"other attempt", func(f *RetrievalFacts) {
			f.Version.Scope.Visibility, f.Version.Scope.AcceptedResultRef = VisibilityAttempt, ""
			f.Version.Scope.AttemptID = "a2"
			f.ExactVersionApproved = false
		}, RetrievalNotVisible, nil},
		{"attempt scope with no current attempt", func(f *RetrievalFacts) {
			f.CurrentAttemptID, f.Version.Scope.Visibility, f.Version.Scope.AcceptedResultRef = "", VisibilityAttempt, ""
			f.ExactVersionApproved = false
		}, RetrievalNotVisible, nil},
		// Effective visibility: a human approval of the exact version makes an
		// attempt observation visible to the whole project.
		{"approved attempt version in another attempt", func(f *RetrievalFacts) {
			f.Version.Trust = TrustAgentClaim
			f.Version.Scope.Visibility, f.Version.Scope.AcceptedResultRef = VisibilityAttempt, ""
			f.Version.Scope.AttemptID = "a2"
		}, RetrievalEligible, nil},
		{"approved attempt version with no current attempt", func(f *RetrievalFacts) {
			f.CurrentAttemptID, f.Version.Scope.Visibility, f.Version.Scope.AcceptedResultRef = "", VisibilityAttempt, ""
		}, RetrievalEligible, nil},
		{"unapproved agent claim in another attempt", func(f *RetrievalFacts) {
			f.Version.Trust, f.ExactVersionApproved = TrustAgentClaim, false
			f.Version.Scope.Visibility, f.Version.Scope.AcceptedResultRef = VisibilityAttempt, ""
			f.Version.Scope.AttemptID = "a2"
		}, RetrievalNotVisible, nil},
		{"human origin in attempt scope", func(f *RetrievalFacts) {
			human(f)
			f.Version.Scope.Visibility = VisibilityAttempt
		}, RetrievalNotVisible, nil},
		{"unapproved human origin in attempt scope", func(f *RetrievalFacts) {
			human(f)
			f.Version.Scope.Visibility, f.ExactVersionApproved = VisibilityAttempt, false
		}, RetrievalNotVisible, nil},
		// An approval does not stand in for the accepted result.
		{"project without accepted result", func(f *RetrievalFacts) { f.Version.Scope.AcceptedResultRef = "" }, RetrievalNotVisible, nil},
		{"human without exact approval", func(f *RetrievalFacts) {
			human(f)
			f.ExactVersionApproved = false
		}, RetrievalNotVisible, nil},

		{"old version", func(f *RetrievalFacts) { f.Version.ID = "v0" }, RetrievalVersionNotCurrent, nil},
		{"index result with old version", func(f *RetrievalFacts) {
			f.Version.ID = "v0"
			f.FromIndex = true
		}, RetrievalStaleIndexResult, nil},
		{"old index version without exact human approval", func(f *RetrievalFacts) {
			human(f)
			f.ExactVersionApproved, f.Version.ID, f.FromIndex = false, "v0", true
		}, RetrievalStaleIndexResult, nil},
		{"old index version without accepted result", func(f *RetrievalFacts) {
			f.Version.Scope.AcceptedResultRef, f.Version.ID, f.FromIndex = "", "v0", true
		}, RetrievalStaleIndexResult, nil},
		{"foreign project stale index hit", func(f *RetrievalFacts) {
			f.Record.ProjectID, f.Version.ID, f.FromIndex = "p2", "v0", true
		}, RetrievalForeignProject, nil},
		{"invalid facts precede stale index hit", func(f *RetrievalFacts) {
			f.Version.Trust, f.Version.ID, f.FromIndex = "score", "v0", true
		}, RetrievalInvalidFacts, nil},
		{"old non-index version stays not visible first", func(f *RetrievalFacts) {
			f.Version.Scope.AcceptedResultRef, f.Version.ID = "", "v0"
		}, RetrievalNotVisible, nil},
		{"current index version stays not visible", func(f *RetrievalFacts) {
			f.Version.Scope.AcceptedResultRef, f.FromIndex = "", true
		}, RetrievalNotVisible, nil},
		{"index result with no version row", func(f *RetrievalFacts) {
			f.FromIndex, f.Version = true, RecordVersion{}
		}, RetrievalStaleIndexResult, nil},
		{"index result without local version", func(f *RetrievalFacts) {
			f.Version.ID = ""
			f.FromIndex = true
		}, RetrievalStaleIndexResult, nil},

		{"proposed", func(f *RetrievalFacts) { f.Record.Status = StatusProposed }, RetrievalLifecycleExcluded, nil},
		{"rejected", func(f *RetrievalFacts) { f.Record.Status = StatusRejected }, RetrievalLifecycleExcluded, nil},
		{"superseded", func(f *RetrievalFacts) { f.Record.Status = StatusSuperseded }, RetrievalLifecycleExcluded, nil},
		{"archived", func(f *RetrievalFacts) { f.Record.Status = StatusArchived }, RetrievalLifecycleExcluded, nil},
		{"proposed human approved trust", func(f *RetrievalFacts) {
			human(f)
			f.Record.Status = StatusProposed
		}, RetrievalLifecycleExcluded, nil},

		{"agent claim", func(f *RetrievalFacts) { f.Version.Trust, f.ExactVersionApproved = TrustAgentClaim, false }, RetrievalAgentClaim, nil},
		{"agent claim without file source", func(f *RetrievalFacts) {
			f.Version.Trust, f.ExactVersionApproved = TrustAgentClaim, false
			f.Applicability = ApplicabilityUnknown
		}, RetrievalAgentClaim, nil},
		{"agent claim precedes applicability", func(f *RetrievalFacts) {
			f.Version.Trust, f.ExactVersionApproved = TrustAgentClaim, false
			f.Applicability = ApplicabilityMismatch
		}, RetrievalAgentClaim, nil},
		// Effective trust: a human approval of the exact version makes an
		// agent claim human_approved without a new version.
		{"exactly approved agent claim", func(f *RetrievalFacts) { f.Version.Trust = TrustAgentClaim }, RetrievalEligible, nil},
		{"exactly approved agent claim without file source", func(f *RetrievalFacts) {
			f.Version.Trust = TrustAgentClaim
			f.Applicability = ApplicabilityUnknown
		}, RetrievalEligible, []RetrievalLabel{LabelApplicabilityUnknown}},
		{"exactly approved agent claim with mismatch", func(f *RetrievalFacts) {
			f.Version.Trust = TrustAgentClaim
			f.Applicability = ApplicabilityMismatch
		}, RetrievalNotApplicable, nil},
		{"exactly approved observation without file source", func(f *RetrievalFacts) { f.Applicability = ApplicabilityUnknown }, RetrievalEligible, []RetrievalLabel{LabelApplicabilityUnknown}},

		{"active with mismatch", func(f *RetrievalFacts) { f.Applicability = ApplicabilityMismatch }, RetrievalNotApplicable, nil},

		{"not human approved without file source", func(f *RetrievalFacts) { f.Applicability, f.ExactVersionApproved = ApplicabilityUnknown, false }, RetrievalNotApplicable, nil},
		{"needs verification not human approved without file source", func(f *RetrievalFacts) {
			f.Record.Status = StatusNeedsVerification
			f.Applicability, f.ExactVersionApproved = ApplicabilityUnknown, false
		}, RetrievalNotApplicable, nil},

		{"needs verification with mismatch", func(f *RetrievalFacts) {
			f.Record.Status = StatusNeedsVerification
			f.Applicability = ApplicabilityMismatch
		}, RetrievalNotApplicable, nil},
		{"active with unavailable source", func(f *RetrievalFacts) { f.Applicability = ApplicabilitySourceUnavailable }, RetrievalEligible, []RetrievalLabel{LabelNeedsVerification}},
		{"needs verification with unavailable source", func(f *RetrievalFacts) {
			f.Record.Status = StatusNeedsVerification
			f.Applicability = ApplicabilitySourceUnavailable
		}, RetrievalEligible, []RetrievalLabel{LabelNeedsVerification}},

		{"foreign project precedes old version", func(f *RetrievalFacts) {
			f.Record.ProjectID = "p2"
			f.Version.ID = "v0"
		}, RetrievalForeignProject, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideRetrievalEligibility(retrievalFacts(tt.change))
			if got.Kind != tt.want || got.Reason == "" || !slices.Equal(got.Labels, tt.wantLabels) {
				t.Fatalf("decision = %+v, want %s labels %v", got, tt.want, tt.wantLabels)
			}
		})
	}
}

// human_approved trust needs an approval of the exact version for every
// origin and visibility. A review that is only an opinion is reported as
// ExactVersionApproved false, like no review at all.
func TestHumanApprovedTrustNeedsExactApproval(t *testing.T) {
	scopes := []struct {
		name  string
		scope Scope
		// unapproved is the kind without exact approval.
		unapproved RetrievalKind
	}{
		{"attempt origin, project with accepted result", Scope{
			ProjectID: "p1", Visibility: VisibilityProject, Origin: OriginAttempt, RunID: "run", AttemptID: "a0", BaseCommit: "commit", AcceptedResultRef: "result",
		}, RetrievalTrustNotApproved},
		{"attempt origin, own attempt", Scope{
			ProjectID: "p1", Visibility: VisibilityAttempt, Origin: OriginAttempt, RunID: "run", AttemptID: "a1", BaseCommit: "commit",
		}, RetrievalTrustNotApproved},
		// Visibility already requires the approval for a human origin.
		{"human origin, project", Scope{ProjectID: "p1", Visibility: VisibilityProject, Origin: OriginHuman}, RetrievalNotVisible},
	}
	approvals := []struct {
		name     string
		approved bool
	}{{"no review", false}, {"opinion only", false}, {"exact approval", true}}
	for _, s := range scopes {
		for _, a := range approvals {
			for _, applicability := range []ApplicabilityKind{ApplicabilityApplicable, ApplicabilityUnknown} {
				t.Run(s.name+"/"+a.name+"/"+string(applicability), func(t *testing.T) {
					got := DecideRetrievalEligibility(retrievalFacts(func(f *RetrievalFacts) {
						f.Version.Trust, f.Version.Scope = TrustHumanApproved, s.scope
						f.ExactVersionApproved, f.Applicability = a.approved, applicability
					}))
					want := s.unapproved
					if a.approved {
						want = RetrievalEligible
					}
					if got.Kind != want || got.Reason == "" {
						t.Fatalf("decision = %+v, want %s", got, want)
					}
				})
			}
		}
	}
	// Lifecycle precedes trust.
	got := DecideRetrievalEligibility(retrievalFacts(func(f *RetrievalFacts) {
		f.Version.Trust, f.ExactVersionApproved, f.Record.Status = TrustHumanApproved, false, StatusArchived
	}))
	if got.Kind != RetrievalLifecycleExcluded {
		t.Fatalf("decision = %+v, want lifecycle_excluded", got)
	}
}
