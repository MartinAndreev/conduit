package provider

import "testing"

func TestAssignmentRefSameIdentity(t *testing.T) {
	base := AssignmentRef{ProjectID: "p", RunID: "r", AttemptID: "a", AssignmentID: "x", Revision: 1}
	revision := base
	revision.Revision = 99
	if !base.SameIdentity(revision) {
		t.Fatal("revision must not affect assignment identity")
	}
	checks := []AssignmentRef{base, base, base, base}
	checks[0].ProjectID = "other"
	checks[1].RunID = "other"
	checks[2].AttemptID = "other"
	checks[3].AssignmentID = "other"
	for i, candidate := range checks {
		if base.SameIdentity(candidate) {
			t.Fatalf("field case %d unexpectedly matched", i)
		}
	}
}
