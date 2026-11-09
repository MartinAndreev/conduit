package memory

// RetrievalFacts are the local data for one candidate. Every candidate is
// evaluated, including ones returned by an index.
type RetrievalFacts struct {
	TargetProjectID  string
	CurrentAttemptID string
	// RecordFound is false when no local record exists for the candidate.
	RecordFound bool
	Record      Record
	Version     RecordVersion
	// ExactVersionApproved is true when a human approved this exact version.
	ExactVersionApproved bool
	// FromIndex is true when the candidate came from a search index.
	FromIndex     bool
	Applicability ApplicabilityKind
}

type RetrievalKind string

const (
	RetrievalEligible          RetrievalKind = "eligible"
	RetrievalInvalidFacts      RetrievalKind = "invalid_facts"
	RetrievalForeignProject    RetrievalKind = "foreign_project"
	RetrievalNotVisible        RetrievalKind = "not_visible"
	RetrievalVersionNotCurrent RetrievalKind = "version_not_current"
	// RetrievalStaleIndexResult is rejected like a non-current version, and
	// procedures count it separately.
	RetrievalStaleIndexResult  RetrievalKind = "stale_index_result"
	RetrievalLifecycleExcluded RetrievalKind = "lifecycle_excluded"
	RetrievalAgentClaim        RetrievalKind = "agent_claim"
	RetrievalNotApplicable     RetrievalKind = "not_applicable"
)

// RetrievalLabel is a visible mark an eligible candidate carries in the manifest.
type RetrievalLabel string

const (
	LabelNeedsVerification    RetrievalLabel = "needs_verification"
	LabelApplicabilityUnknown RetrievalLabel = "applicability_unknown"
)

type RetrievalDecision struct {
	Kind   RetrievalKind
	Reason string
	Labels []RetrievalLabel
}

// DecideRetrievalEligibility answers whether a candidate may enter composition.
// Checks run in the spec's order: project, visibility, current version,
// lifecycle, applicability. An index result that is not the current version is
// classified before visibility, so every stale hit is counted.
func DecideRetrievalEligibility(f RetrievalFacts) RetrievalDecision {
	if !f.RecordFound {
		if f.FromIndex {
			return RetrievalDecision{Kind: RetrievalStaleIndexResult, Reason: "index result has no local record"}
		}
		return RetrievalDecision{Kind: RetrievalInvalidFacts, Reason: "candidate has no local record and did not come from an index"}
	}
	if f.FromIndex && f.Version.ID == "" {
		return RetrievalDecision{Kind: RetrievalStaleIndexResult, Reason: "index result has no local version"}
	}
	scope := f.Version.Scope
	if f.TargetProjectID == "" || f.Record.ProjectID == "" || !validStatus(f.Record.Status) || !validTrust(f.Version.Trust) ||
		!validVisibility(scope.Visibility) || !validOrigin(scope.Origin) {
		return RetrievalDecision{Kind: RetrievalInvalidFacts, Reason: "project, status, trust, or scope is missing or unknown"}
	}
	if !validOriginFields(scope) {
		return RetrievalDecision{Kind: RetrievalInvalidFacts, Reason: "attempt origin needs run, attempt and base commit; human origin has no run or attempt"}
	}
	if f.Record.ProjectID != f.TargetProjectID || scope.ProjectID != f.TargetProjectID {
		return RetrievalDecision{Kind: RetrievalForeignProject, Reason: "candidate belongs to another project"}
	}
	if f.FromIndex && f.Version.ID != f.Record.CurrentVersionID {
		return RetrievalDecision{Kind: RetrievalStaleIndexResult, Reason: "index result has no matching current version"}
	}
	if visible, reason := visibleToAttempt(f); !visible {
		return RetrievalDecision{Kind: RetrievalNotVisible, Reason: reason}
	}
	if f.Version.ID == "" || f.Version.ID != f.Record.CurrentVersionID {
		return RetrievalDecision{Kind: RetrievalVersionNotCurrent, Reason: "candidate is not the record's current version"}
	}
	if f.Record.Status != StatusActive && f.Record.Status != StatusNeedsVerification {
		return RetrievalDecision{Kind: RetrievalLifecycleExcluded, Reason: "only active and needs_verification records are retrievable"}
	}
	if f.Version.Trust == TrustAgentClaim {
		return RetrievalDecision{Kind: RetrievalAgentClaim, Reason: "agent claims stay out until reviewed"}
	}
	return decideRetrievalApplicability(f)
}

func validOriginFields(scope Scope) bool {
	if scope.Origin == OriginHuman {
		return scope.RunID == "" && scope.AttemptID == ""
	}
	return scope.RunID != "" && scope.AttemptID != "" && scope.BaseCommit != ""
}

func visibleToAttempt(f RetrievalFacts) (bool, string) {
	scope := f.Version.Scope
	switch {
	case scope.Visibility == VisibilityAttempt && scope.Origin == OriginHuman:
		return false, "human decisions do not pass through an attempt"
	case scope.Visibility == VisibilityAttempt && scope.AttemptID != f.CurrentAttemptID:
		return false, "attempt-scoped version is visible only in its own attempt"
	case scope.Visibility == VisibilityProject && scope.Origin == OriginAttempt && scope.AcceptedResultRef == "":
		return false, "attempt observation reaches the project only through an accepted result"
	case scope.Visibility == VisibilityProject && scope.Origin == OriginHuman && !f.ExactVersionApproved:
		return false, "human version reaches the project only through approval of this exact version"
	default:
		return true, ""
	}
}

func decideRetrievalApplicability(f RetrievalFacts) RetrievalDecision {
	needsVerification := f.Record.Status == StatusNeedsVerification
	var labels []RetrievalLabel
	if needsVerification {
		labels = append(labels, LabelNeedsVerification)
	}
	switch f.Applicability {
	case ApplicabilityApplicable:
		return RetrievalDecision{Kind: RetrievalEligible, Reason: "candidate is current, visible and applicable", Labels: labels}
	case ApplicabilityMismatch:
		return RetrievalDecision{Kind: RetrievalNotApplicable, Reason: "a declared source differs from the target snapshot"}
	case ApplicabilitySourceUnavailable:
		return RetrievalDecision{Kind: RetrievalEligible, Reason: "a missing or unreadable source enters labelled needs_verification", Labels: []RetrievalLabel{LabelNeedsVerification}}
	case ApplicabilityUnknown:
		if f.Version.Trust == TrustHumanApproved {
			return RetrievalDecision{Kind: RetrievalEligible, Reason: "human approved record without a file source enters with a label", Labels: append(labels, LabelApplicabilityUnknown)}
		}
		return RetrievalDecision{Kind: RetrievalNotApplicable, Reason: "only human approved records enter without a file source"}
	default:
		return RetrievalDecision{Kind: RetrievalInvalidFacts, Reason: "applicability is missing or invalid"}
	}
}
