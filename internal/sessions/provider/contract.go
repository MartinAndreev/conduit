package provider

import (
	"context"
	"encoding/json"
	"time"
)

// Provider adapts an agent harness. Implementations belong in harness adapter
// packages; model inference providers are selected through ModelSelection.
type Provider interface {
	Inspect(context.Context, InspectRequest) (ProviderDescriptor, error)
	OpenSession(context.Context, OpenSessionRequest) (OpenSessionResult, error)
}

// ResumableProvider is an optional extension. Consumers must also require a
// supported session.resume capability before using it.
type ResumableProvider interface {
	Provider
	ResumeSession(context.Context, ResumeSessionRequest) (OpenSessionResult, error)
}

// Session controls one adapter session. At most one turn may be active. Context
// cancellation stops a caller's wait and does not itself cancel remote work.
type Session interface {
	Ref() SessionRef
	Capabilities() CapabilitySnapshot
	Events(context.Context, EventCursor) (EventStream, error)
	ObserveState(context.Context, ObserveStateRequest) (SessionSnapshot, error)
	StartTurn(context.Context, StartTurnRequest) (StartTurnResult, error)
	Steer(context.Context, SteerRequest) (DeliveryReceipt, error)
	RespondPermission(context.Context, PermissionResponse) (DeliveryReceipt, error)
	AnswerQuestion(context.Context, QuestionAnswer) (DeliveryReceipt, error)
	InterruptTurn(context.Context, InterruptRequest) (InterruptResult, error)
	Close(context.Context, CloseSessionRequest) (CloseSessionResult, error)
}

// EventStream receives normalized adapter observations. Closing or cancelling a
// receive only unsubscribes the consumer; it does not stop the turn or session.
type EventStream interface {
	Receive(context.Context) (ProviderEvent, error)
	Close() error
}

type InspectRequest struct {
	ExecutablePath string
	Model          ModelSelection
}

type ProviderDescriptor struct {
	ProviderName    string
	AdapterVersion  string
	HarnessVersion  string
	ProtocolVersion string
	Capabilities    CapabilitySnapshot
}

type ModelSelection struct {
	ProviderID string
	ModelID    string
	Variant    string
}

type WorkspaceBinding struct {
	WorkspaceID WorkspaceID
	Path        string
	Writable    bool
}

type OpenSessionRequest struct {
	RequestID      RequestID
	Assignment     AssignmentRef
	Workspace      WorkspaceBinding
	Model          ModelSelection
	ResultContract ResultContractRef
	PermissionMode PermissionMode
}

type OpenSessionResult struct {
	Session  Session
	Contract ResultContractNegotiation
}

type ResumeSessionRequest struct {
	RequestID      RequestID
	Assignment     AssignmentRef
	ResumeToken    ResumeToken
	Workspace      WorkspaceBinding
	ResultContract ResultContractRef
}

type InputPart struct {
	Kind      InputPartKind
	Text      string
	URI       string
	MediaType string
}

type StartTurnRequest struct {
	RequestID      RequestID
	Session        SessionRef
	Assignment     AssignmentRef
	Input          []InputPart
	ResultContract ResultContractRef
}

type StartTurnResult struct {
	Turn              TurnRef
	Assignment        AssignmentRef
	TargetingStrength TargetingStrength
	ResultEnforcement SchemaEnforcement
	Native            NativeIDs
}

// SteerRequest carries both sides of a Conduit-owned revision transition. The
// adapter records only a revision it actually dispatched. Unknown delivery must
// retain both bindings for reconciliation and must not relabel later candidates.
type SteerRequest struct {
	RequestID          RequestID
	MessageID          MessageID
	Session            SessionRef
	ExpectedActiveTurn TurnRef
	CurrentAssignment  AssignmentRef
	ExpectedRevision   uint64
	NewAssignment      AssignmentRef
	Mode               DeliveryMode
	Parts              []InputPart
}

type PermissionResponse struct {
	RequestID    RequestID
	Session      SessionRef
	Turn         TurnRef
	Assignment   AssignmentRef
	PermissionID PermissionID
	Decision     PermissionDecision
}

type QuestionAnswer struct {
	RequestID  RequestID
	Session    SessionRef
	Turn       TurnRef
	Assignment AssignmentRef
	QuestionID QuestionID
	Parts      []InputPart
}

type InterruptReason struct {
	Code   string
	Detail string
}

type InterruptRequest struct {
	RequestID          RequestID
	Session            SessionRef
	ExpectedActiveTurn TurnRef
	Assignment         AssignmentRef
	Requirement        InterruptRequirement
	Reason             InterruptReason
}

type InterruptResult struct {
	StopOutcome       StopOutcome
	ObservedUsability ObservedSessionUsability
	Quiescence        QuiescenceState
}

type CloseSessionRequest struct {
	RequestID RequestID
	Session   SessionRef
	Policy    ClosePolicy
}

type CloseSessionResult struct {
	HandleClosed      bool
	ExecutionState    ExecutionState
	ResourcesReleased bool
}

type ObserveStateRequest struct {
	Session SessionRef
}

type PendingRequestRef struct {
	Kind     PendingRequestKind
	NativeID string
}

type SessionSnapshot struct {
	Knowledge       SnapshotKnowledge
	SessionState    SessionState
	ActiveTurn      Optional[TurnRef]
	Native          NativeIDs
	NativeSequence  Optional[uint64]
	PendingRequests []PendingRequestRef
	ObservedAt      time.Time
}

// DeliveryReceipt reports transport knowledge only. It never proves that an
// agent applied a correction.
type DeliveryReceipt struct {
	RequestID       RequestID
	MessageID       MessageID
	Disposition     DeliveryDisposition
	Receipt         ReceiptKnowledge
	Targeting       TargetingStrength
	DispatchedFor   Optional[AssignmentRef]
	NativeReceiptID Optional[NativeReceiptID]
	ObservedAt      time.Time
}

// ResultContractRef requests one enforcement mode and lists every fallback the
// caller explicitly permits. An adapter must reject an unlisted actual mode.
type ResultContractRef struct {
	Name             string
	Version          string
	Schema           json.RawMessage
	Requested        SchemaEnforcement
	AllowedFallbacks []SchemaEnforcement
}

// Allows reports whether actual is the requested enforcement or one of the
// caller's explicit fallbacks. It never invents a fallback for an empty list.
func (r ResultContractRef) Allows(actual SchemaEnforcement) bool {
	if !isKnownSchemaEnforcement(actual) {
		return false
	}
	if actual == r.Requested {
		return isKnownSchemaEnforcement(r.Requested)
	}
	for _, allowed := range r.AllowedFallbacks {
		if isKnownSchemaEnforcement(allowed) && actual == allowed {
			return true
		}
	}
	return false
}

func isKnownSchemaEnforcement(value SchemaEnforcement) bool {
	switch value {
	case SchemaNative, SchemaPromptOnly, SchemaNone:
		return true
	default:
		return false
	}
}

type ResultContractNegotiation struct {
	Contract ResultContractRef
	Actual   SchemaEnforcement
}

// ResultCandidate is untrusted transport input. Capturing one is not a Conduit
// completion decision. An absent Assignment means attribution is unresolved;
// consumers must not interpret a zero revision as the current revision.
type ResultCandidate struct {
	SubmissionID SubmissionID
	Assignment   Optional[AssignmentRef]
	Source       CandidateSource
	Contract     ResultContractRef
	Enforcement  SchemaEnforcement
	Payload      json.RawMessage
	EvidenceRefs []EvidenceRef
	Native       NativeIDs
}

type EvidenceRef struct {
	Kind string
	ID   EvidenceID
}

type EventCursor struct {
	AdapterCursor string
}

type CapabilitySnapshot struct {
	ProviderName    string
	AdapterVersion  string
	HarnessVersion  string
	ProtocolVersion string
	Model           ModelSelection
	ObservedAt      time.Time
	Items           []CapabilityObservation
}

type CapabilityObservation struct {
	Capability Capability
	Support    SupportState
	Evidence   EvidenceLevel
	Targeting  TargetingStrength
	Detail     string
}
