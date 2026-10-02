package sessions

import "testing"

func TestCorrectionRule(t *testing.T) {
	current := AssignmentRef{ProjectID: "p", RunID: "r", AttemptID: "a", AssignmentID: "x", Revision: 4}
	next := current
	next.Revision++
	tests := []struct {
		name   string
		mutate func(*AssignmentRef, *AssignmentRef, *uint64, *AssignmentRef)
		kind   CorrectionKind
	}{
		{"exact next revision", func(_, _ *AssignmentRef, _ *uint64, _ *AssignmentRef) {}, CorrectionAllowed},
		{"stale current revision", func(_, supplied *AssignmentRef, _ *uint64, _ *AssignmentRef) { supplied.Revision-- }, CorrectionStaleCurrent},
		{"stale expected revision", func(_, _ *AssignmentRef, expected *uint64, _ *AssignmentRef) { *expected-- }, CorrectionStaleCurrent},
		{"stale current identity", func(_, supplied *AssignmentRef, _ *uint64, _ *AssignmentRef) { supplied.AssignmentID = "other" }, CorrectionStaleCurrent},
		{"project identity", func(_, _ *AssignmentRef, _ *uint64, next *AssignmentRef) { next.ProjectID = "other" }, CorrectionIdentityChanged},
		{"run identity", func(_, _ *AssignmentRef, _ *uint64, next *AssignmentRef) { next.RunID = "other" }, CorrectionIdentityChanged},
		{"attempt identity", func(_, _ *AssignmentRef, _ *uint64, next *AssignmentRef) { next.AttemptID = "other" }, CorrectionIdentityChanged},
		{"assignment identity", func(_, _ *AssignmentRef, _ *uint64, next *AssignmentRef) { next.AssignmentID = "other" }, CorrectionIdentityChanged},
		{"equal revision", func(_, _ *AssignmentRef, _ *uint64, next *AssignmentRef) { next.Revision = 4 }, CorrectionInvalidNextRevision},
		{"lower revision", func(_, _ *AssignmentRef, _ *uint64, next *AssignmentRef) { next.Revision = 3 }, CorrectionInvalidNextRevision},
		{"skipped revision", func(_, _ *AssignmentRef, _ *uint64, next *AssignmentRef) { next.Revision = 6 }, CorrectionInvalidNextRevision},
		{"overflow", func(current, supplied *AssignmentRef, expected *uint64, next *AssignmentRef) {
			current.Revision = ^uint64(0)
			*supplied = *current
			*expected = current.Revision
			next.Revision = 0
		}, CorrectionRevisionExhausted},
		{"last representable revision", func(current, supplied *AssignmentRef, expected *uint64, next *AssignmentRef) {
			current.Revision = ^uint64(0) - 1
			*supplied = *current
			*expected = current.Revision
			next.Revision = ^uint64(0)
		}, CorrectionAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			facts, supplied, candidate, expected := current, current, next, current.Revision
			tc.mutate(&facts, &supplied, &expected, &candidate)
			decision := DecideCorrection(facts, supplied, expected, candidate)
			if decision.Kind != tc.kind || decision.Reason == "" {
				t.Fatalf("decision=%#v, want %s and reason", decision, tc.kind)
			}
		})
	}
}
