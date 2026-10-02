package provider

import (
	"errors"
	"time"
)

type EventKind string

const (
	EventSessionState        EventKind = "session_state"
	EventTurnState           EventKind = "turn_state"
	EventAssistantText       EventKind = "assistant_text"
	EventReasoningSummary    EventKind = "reasoning_summary"
	EventToolStarted         EventKind = "tool_started"
	EventToolUpdated         EventKind = "tool_updated"
	EventToolCompleted       EventKind = "tool_completed"
	EventCommandStarted      EventKind = "command_started"
	EventCommandOutput       EventKind = "command_output"
	EventCommandCompleted    EventKind = "command_completed"
	EventFileActivity        EventKind = "file_activity"
	EventPermissionRequested EventKind = "permission_requested"
	EventQuestionAsked       EventKind = "question_asked"
	EventControlReceipt      EventKind = "control_receipt"
	EventResultCandidate     EventKind = "result_candidate"
	EventUsage               EventKind = "usage"
	EventGap                 EventKind = "event_gap"
	EventProtocolWarning     EventKind = "protocol_warning"
)

// ErrInvalidEventBody indicates a nil, pointer, or foreign event body. Event
// bodies are value types from this package so a typed nil cannot enter the union.
var ErrInvalidEventBody = errors.New("invalid provider event body")

// EventEnvelope carries adapter-local ordering and optional native correlation.
// AdapterSeq is ordered only within one opened session. Durable run ordering and
// replay belong to Conduit's event journal.
type EventEnvelope struct {
	EventID    EventID
	Session    SessionRef
	Turn       Optional[TurnRef]
	Assignment Optional[AssignmentRef]
	AdapterSeq uint64
	NativeSeq  Optional[uint64]
	Native     NativeIDs
	ObservedAt time.Time
}

// ProviderEvent is a normalized harness observation. Its kind is derived from a
// closed typed body, so adapters cannot construct a kind/body mismatch.
type ProviderEvent struct {
	Envelope EventEnvelope
	body     EventBody
}

// NewProviderEvent constructs an event from one of the package's value-type
// bodies. Pointer bodies are rejected to exclude typed-nil interface values.
func NewProviderEvent(envelope EventEnvelope, body EventBody) (ProviderEvent, error) {
	if _, ok := eventKindOf(body); !ok {
		return ProviderEvent{}, ErrInvalidEventBody
	}
	return ProviderEvent{Envelope: envelope, body: body}, nil
}

// Kind returns the event kind implied by Body.
func (e ProviderEvent) Kind() EventKind {
	kind, _ := eventKindOf(e.body)
	return kind
}

// Body returns the closed typed event body.
func (e ProviderEvent) Body() EventBody {
	return e.body
}

// EventBody is a closed sum-type convention. External adapter packages can use
// the concrete variants but cannot add arbitrary variants.
type EventBody interface {
	providerEventBody()
}

type SessionStateEvent struct {
	Previous     SessionState
	Current      SessionState
	NativeStatus string
}

type TurnStateEvent struct {
	Previous       TurnState
	Current        TurnState
	TerminalReason string
}

// TextSegmentUpdate contains enough identity and version data to merge deltas
// without joining interleaved messages. A snapshot replaces one segment version;
// a delta applies only to its stated base version and byte offset. A truncated
// update creates an incomplete segment range: consumers must not splice later
// deltas over the missing bytes and need a complete later snapshot to recover.
type TextSegmentUpdate struct {
	ItemID        ItemID
	SegmentID     SegmentID
	Text          string
	Mode          UpdateMode
	BaseVersion   Optional[uint64]
	Version       uint64
	ByteOffset    uint64
	Truncated     bool
	OriginalBytes Optional[uint64]
}

type AssistantTextEvent struct {
	Phase  AssistantPhase
	Update TextSegmentUpdate
}

// ReasoningSummaryEvent contains only reasoning text exposed by the harness. It
// does not promise access to a model's private chain of thought.
type ReasoningSummaryEvent struct {
	Availability ReasoningAvailability
	Update       TextSegmentUpdate
}

// BoundedText records truncation explicitly. OriginalBytes may be unknown.
type BoundedText struct {
	Text          string
	Truncated     bool
	OriginalBytes Optional[uint64]
}

type ToolStartedEvent struct {
	ToolCallID NativeToolCallID
	ToolName   string
	Arguments  BoundedText
}

type ToolUpdatedEvent struct {
	ToolCallID NativeToolCallID
	Output     BoundedText
	Mode       UpdateMode
}

type ToolCompletedEvent struct {
	ToolCallID NativeToolCallID
	Outcome    ToolOutcome
	Output     BoundedText
}

type CommandStartedEvent struct {
	CommandID NativeCommandID
	Argv      []string
	Display   BoundedText
	CWD       string
}

type CommandOutputEvent struct {
	CommandID NativeCommandID
	Stream    CommandStream
	Output    BoundedText
	Mode      UpdateMode
}

type CommandCompletedEvent struct {
	CommandID NativeCommandID
	ExitCode  Optional[int]
	Signal    string
	Output    BoundedText
}

// FileActivityEvent is a native harness observation, not an independent
// filesystem or Git verification by Conduit.
type FileActivityEvent struct {
	Operation  FileOperation
	NativePath string
	ToolCallID Optional[NativeToolCallID]
}

type PermissionScope struct {
	Kind     PermissionScopeKind
	Resource string
}

type PermissionRequestedEvent struct {
	PermissionID PermissionID
	Scope        PermissionScope
	Rationale    string
}

type QuestionChoice struct {
	ID    string
	Label string
}

type QuestionAskedEvent struct {
	QuestionID QuestionID
	Prompt     string
	Choices    []QuestionChoice
	Blocking   bool
}

type ControlReceiptEvent struct {
	Receipt DeliveryReceipt
}

type ResultCandidateEvent struct {
	Candidate ResultCandidate
}

// UsageCounters preserve unknown separately from a known zero. Cost is a native
// provider-reported value and does not itself prove billing.
type UsageCounters struct {
	InputTokens      Optional[uint64]
	OutputTokens     Optional[uint64]
	ReasoningTokens  Optional[uint64]
	CacheReadTokens  Optional[uint64]
	CacheWriteTokens Optional[uint64]
	Cost             Optional[float64]
	Currency         string
}

type UsageEvent struct {
	Scope       UsageScope
	ScopeID     string
	Measurement UsageMeasurement
	Model       ModelSelection
	Counters    UsageCounters
}

type EventGapEvent struct {
	ExpectedAdapterSeq Optional[uint64]
	ActualAdapterSeq   Optional[uint64]
	ExpectedNativeSeq  Optional[uint64]
	ActualNativeSeq    Optional[uint64]
	Cause              string
	AffectedKinds      []EventKind
}

type ProtocolWarningEvent struct {
	Code                string
	Detail              BoundedText
	OffendingNativeKind string
}

func (SessionStateEvent) providerEventBody()        {}
func (TurnStateEvent) providerEventBody()           {}
func (AssistantTextEvent) providerEventBody()       {}
func (ReasoningSummaryEvent) providerEventBody()    {}
func (ToolStartedEvent) providerEventBody()         {}
func (ToolUpdatedEvent) providerEventBody()         {}
func (ToolCompletedEvent) providerEventBody()       {}
func (CommandStartedEvent) providerEventBody()      {}
func (CommandOutputEvent) providerEventBody()       {}
func (CommandCompletedEvent) providerEventBody()    {}
func (FileActivityEvent) providerEventBody()        {}
func (PermissionRequestedEvent) providerEventBody() {}
func (QuestionAskedEvent) providerEventBody()       {}
func (ControlReceiptEvent) providerEventBody()      {}
func (ResultCandidateEvent) providerEventBody()     {}
func (UsageEvent) providerEventBody()               {}
func (EventGapEvent) providerEventBody()            {}
func (ProtocolWarningEvent) providerEventBody()     {}

func eventKindOf(body EventBody) (EventKind, bool) {
	switch body.(type) {
	case SessionStateEvent:
		return EventSessionState, true
	case TurnStateEvent:
		return EventTurnState, true
	case AssistantTextEvent:
		return EventAssistantText, true
	case ReasoningSummaryEvent:
		return EventReasoningSummary, true
	case ToolStartedEvent:
		return EventToolStarted, true
	case ToolUpdatedEvent:
		return EventToolUpdated, true
	case ToolCompletedEvent:
		return EventToolCompleted, true
	case CommandStartedEvent:
		return EventCommandStarted, true
	case CommandOutputEvent:
		return EventCommandOutput, true
	case CommandCompletedEvent:
		return EventCommandCompleted, true
	case FileActivityEvent:
		return EventFileActivity, true
	case PermissionRequestedEvent:
		return EventPermissionRequested, true
	case QuestionAskedEvent:
		return EventQuestionAsked, true
	case ControlReceiptEvent:
		return EventControlReceipt, true
	case ResultCandidateEvent:
		return EventResultCandidate, true
	case UsageEvent:
		return EventUsage, true
	case EventGapEvent:
		return EventGap, true
	case ProtocolWarningEvent:
		return EventProtocolWarning, true
	default:
		return "", false
	}
}
