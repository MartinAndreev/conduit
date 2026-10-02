package sessions

// Conduit work identities are distinct from native harness identifiers.
type (
	ProjectID    string
	RunID        string
	AttemptID    string
	AssignmentID string
)

// AssignmentRef identifies Conduit-owned work at a specific revision.
type AssignmentRef struct {
	ProjectID    ProjectID
	RunID        RunID
	AttemptID    AttemptID
	AssignmentID AssignmentID
	Revision     uint64
}

// SameIdentity compares durable identity and deliberately excludes Revision.
func (a AssignmentRef) SameIdentity(other AssignmentRef) bool {
	return a.ProjectID == other.ProjectID && a.RunID == other.RunID &&
		a.AttemptID == other.AttemptID && a.AssignmentID == other.AssignmentID
}
