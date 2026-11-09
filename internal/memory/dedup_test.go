package memory

import "testing"

func TestDecideDeduplication(t *testing.T) {
	project := DeduplicationSubject{ProjectID: "p1", Visibility: VisibilityProject}
	attempt := DeduplicationSubject{ProjectID: "p1", Visibility: VisibilityAttempt, AttemptID: "a1"}
	otherAttempt := DeduplicationSubject{ProjectID: "p1", Visibility: VisibilityAttempt, AttemptID: "a2"}
	otherProject := DeduplicationSubject{ProjectID: "p2", Visibility: VisibilityProject}
	tests := []struct {
		name    string
		left    DeduplicationSubject
		right   DeduplicationSubject
		matches bool
		want    DeduplicationKind
	}{
		{"same project visibility", project, project, true, DeduplicationDuplicate},
		{"same attempt", attempt, attempt, true, DeduplicationDuplicate},
		{"no content match", project, project, false, DeduplicationDistinct},
		{"no match across projects", project, otherProject, false, DeduplicationDistinct},
		{"different project", project, otherProject, true, DeduplicationMergeHint},
		{"different visibility", project, attempt, true, DeduplicationMergeHint},
		{"different attempts", attempt, otherAttempt, true, DeduplicationMergeHint},
		{"missing project", DeduplicationSubject{Visibility: VisibilityProject}, project, true, DeduplicationInvalidFacts},
		{"unknown visibility", project, DeduplicationSubject{ProjectID: "p1"}, true, DeduplicationInvalidFacts},
		{"attempt scope without attempt", DeduplicationSubject{ProjectID: "p1", Visibility: VisibilityAttempt}, attempt, true, DeduplicationInvalidFacts},
		{"invalid facts precede no match", DeduplicationSubject{}, project, false, DeduplicationInvalidFacts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideDeduplication(DeduplicationFacts{Left: tt.left, Right: tt.right, ContentMatches: tt.matches})
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}
