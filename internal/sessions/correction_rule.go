package sessions

// CorrectionKind describes the business decision for an assignment correction.
type CorrectionKind string

const (
	CorrectionAllowed             CorrectionKind = "allowed"
	CorrectionStaleCurrent        CorrectionKind = "stale_current"
	CorrectionIdentityChanged     CorrectionKind = "identity_changed"
	CorrectionRevisionExhausted   CorrectionKind = "revision_exhausted"
	CorrectionInvalidNextRevision CorrectionKind = "invalid_next_revision"
)

// CorrectionDecision is an outcome, not a technical error. Reason is explanatory;
// procedures branch on Kind. Zero identities retain their existing meaning here:
// this rule checks a transition, not whether the original assignment is valid.
type CorrectionDecision struct {
	Kind   CorrectionKind
	Reason string
}

// DecideCorrection uses the currently reconciled assignment as supplied facts.
// The caller must keep those facts stable until it reserves the correction.
func DecideCorrection(current, supplied AssignmentRef, expectedRevision uint64, next AssignmentRef) CorrectionDecision {
	if supplied != current || expectedRevision != current.Revision {
		return CorrectionDecision{Kind: CorrectionStaleCurrent, Reason: "current assignment or expected revision differs from the reconciled assignment"}
	}
	if !current.SameIdentity(next) {
		return CorrectionDecision{Kind: CorrectionIdentityChanged, Reason: "correction must preserve assignment identity"}
	}
	if current.Revision == ^uint64(0) {
		return CorrectionDecision{Kind: CorrectionRevisionExhausted, Reason: "assignment revision cannot advance beyond uint64 maximum"}
	}
	if next.Revision != current.Revision+1 {
		return CorrectionDecision{Kind: CorrectionInvalidNextRevision, Reason: "correction must advance assignment revision by exactly one"}
	}
	return CorrectionDecision{Kind: CorrectionAllowed, Reason: "assignment identity is unchanged and revision advances by one"}
}
