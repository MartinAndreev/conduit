package provider

import "github.com/MartinAndreev/conduit/internal/sessions"

// Domain identifiers belong to Conduit and must not be substituted with native
// harness identifiers.
type (
	RequestID    string
	MessageID    string
	ProjectID    = sessions.ProjectID
	RunID        = sessions.RunID
	AttemptID    = sessions.AttemptID
	AssignmentID = sessions.AssignmentID
	WorkspaceID  string
	EvidenceID   string
)

// Adapter identifiers are opaque handles issued by a harness adapter.
type (
	AdapterSessionID string
	AdapterTurnID    string
	ResumeToken      string
)

// Native identifiers only correlate harness observations. They are not domain
// identity and may be unavailable.
type (
	NativeSessionID  string
	NativeTurnID     string
	NativeCommandID  string
	NativeToolCallID string
	NativeReceiptID  string
)

// Protocol identifiers correlate typed events and interactions.
type (
	EventID      string
	ItemID       string
	SegmentID    string
	PermissionID string
	QuestionID   string
	SubmissionID string
)

// Optional distinguishes an unknown or absent value from its type's zero value.
// Adapters should use Some and None rather than inferring presence from Value.
type Optional[T any] struct {
	Value   T
	Present bool
}

// Some returns a present optional value, including a known zero value.
func Some[T any](value T) Optional[T] {
	return Optional[T]{Value: value, Present: true}
}

// None returns an optional value whose value is unknown or absent.
func None[T any]() Optional[T] {
	return Optional[T]{}
}

// AssignmentRef is the domain-owned identity and revision of Conduit work.
type AssignmentRef = sessions.AssignmentRef

// SessionRef is an adapter-issued session handle.
type SessionRef struct {
	AdapterSessionID AdapterSessionID
}

// TurnRef is an adapter-issued turn handle. It may wrap a native turn ID or a
// locally serialized lease when the harness only supports session targeting.
type TurnRef struct {
	AdapterTurnID AdapterTurnID
}

// NativeIDs contains optional native correlation identifiers.
type NativeIDs struct {
	Session  Optional[NativeSessionID]
	Turn     Optional[NativeTurnID]
	Command  Optional[NativeCommandID]
	ToolCall Optional[NativeToolCallID]
}
