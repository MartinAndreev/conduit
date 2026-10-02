package provider

type InputPartKind string

const (
	InputInstruction InputPartKind = "instruction"
	InputContext     InputPartKind = "context"
	InputArtifactRef InputPartKind = "artifact_ref"
)

type PermissionMode string

const (
	PermissionDeny          PermissionMode = "deny"
	PermissionAsk           PermissionMode = "ask"
	PermissionAllowByPolicy PermissionMode = "allow_by_policy"
)

type PermissionDecision string

const (
	PermissionAllowOnce PermissionDecision = "allow_once"
	PermissionDenyOnce  PermissionDecision = "deny"
)

type DeliveryMode string

const (
	DeliverActiveTurn   DeliveryMode = "active_turn"
	DeliverNextBoundary DeliveryMode = "next_boundary"
)

type TargetingStrength string

const (
	TargetNativeTurnCAS TargetingStrength = "native_turn_compare_and_swap"
	TargetNativeTurnID  TargetingStrength = "native_turn_id"
	TargetSessionLease  TargetingStrength = "session_scope_with_adapter_lease"
	TargetUnknown       TargetingStrength = "unknown"
)

type SupportState string

const (
	SupportSupported   SupportState = "supported"
	SupportUnsupported SupportState = "unsupported"
	SupportUnknown     SupportState = "unknown"
)

type EvidenceLevel string

const (
	EvidenceDeclared        EvidenceLevel = "declared"
	EvidenceFixtureVerified EvidenceLevel = "fixture_verified"
	EvidenceLiveObserved    EvidenceLevel = "live_observed"
)

type Capability string

const (
	CapabilityEventText           Capability = "event.text"
	CapabilityEventTool           Capability = "event.tool"
	CapabilityEventCommand        Capability = "event.command"
	CapabilityEventFile           Capability = "event.file"
	CapabilityEventReasoning      Capability = "event.reasoning_summary"
	CapabilitySteerActiveTurn     Capability = "steer.active_turn"
	CapabilitySteerNextBoundary   Capability = "steer.next_boundary"
	CapabilityTurnInterrupt       Capability = "turn.interrupt"
	CapabilitySessionResume       Capability = "session.resume"
	CapabilitySessionObserveState Capability = "session.observe_state"
	CapabilityPermissionRoundTrip Capability = "permission.round_trip"
	CapabilityQuestionRoundTrip   Capability = "question.round_trip"
	CapabilityResultNativeSchema  Capability = "result.native_schema"
	CapabilityEventsNativeReplay  Capability = "events.native_replay"
)

type SchemaEnforcement string

const (
	SchemaNative     SchemaEnforcement = "native_schema"
	SchemaPromptOnly SchemaEnforcement = "prompt_only"
	SchemaNone       SchemaEnforcement = "none"
)

type DeliveryDisposition string

const (
	DeliveryTransportAccepted DeliveryDisposition = "transport_accepted"
	DeliveryQueuedByHarness   DeliveryDisposition = "queued_by_harness"
	DeliveryRejected          DeliveryDisposition = "rejected"
	DeliveryUnknown           DeliveryDisposition = "delivery_unknown"
)

type ReceiptKnowledge string

const (
	ReceiptNativeKnown ReceiptKnowledge = "native_receipt_known"
	ReceiptNone        ReceiptKnowledge = "no_native_receipt"
	ReceiptUnknown     ReceiptKnowledge = "unknown"
)

type InterruptRequirement string

const (
	InterruptPreserveSession InterruptRequirement = "preserve_session"
	InterruptBestEffort      InterruptRequirement = "best_effort"
)

type ObservedSessionUsability string

const (
	SessionUsable           ObservedSessionUsability = "usable"
	SessionUnusable         ObservedSessionUsability = "unusable"
	SessionUsabilityUnknown ObservedSessionUsability = "unknown"
)

type StopOutcome string

const (
	StopStopped      StopOutcome = "stopped"
	StopStillRunning StopOutcome = "still_running"
	StopNotFound     StopOutcome = "not_found"
	StopUnknown      StopOutcome = "unknown"
)

type QuiescenceState string

const (
	Quiescent         QuiescenceState = "quiescent"
	NotQuiescent      QuiescenceState = "not_quiescent"
	QuiescenceUnknown QuiescenceState = "unknown"
)

type ClosePolicy string

const (
	CloseRequireQuiescent   ClosePolicy = "require_quiescent"
	CloseInterruptThenClose ClosePolicy = "interrupt_then_close"
	CloseExplicitDetach     ClosePolicy = "explicit_detach"
)

type ExecutionState string

const (
	ExecutionNone     ExecutionState = "none"
	ExecutionIdle     ExecutionState = "idle"
	ExecutionActive   ExecutionState = "active"
	ExecutionTerminal ExecutionState = "terminal"
	ExecutionUnknown  ExecutionState = "unknown"
)

type SnapshotKnowledge string

const (
	SnapshotKnown   SnapshotKnowledge = "known"
	SnapshotPartial SnapshotKnowledge = "partial"
	SnapshotUnknown SnapshotKnowledge = "unknown"
)

type SessionState string

const (
	SessionOpening      SessionState = "opening"
	SessionOpen         SessionState = "open"
	SessionIdleObserved SessionState = "idle_observed"
	SessionClosing      SessionState = "closing"
	SessionClosed       SessionState = "closed"
	SessionLost         SessionState = "lost"
	SessionStateUnknown SessionState = "unknown"
)

type TurnState string

const (
	TurnStarting           TurnState = "starting"
	TurnActive             TurnState = "active"
	TurnInterruptRequested TurnState = "interrupt_requested"
	TurnNativeTerminal     TurnState = "native_terminal"
	TurnLost               TurnState = "lost"
	TurnStateUnknown       TurnState = "unknown"
)

type UsageScope string

const (
	UsagePrimaryTurn      UsageScope = "primary_turn"
	UsageAuxiliaryTitle   UsageScope = "auxiliary_title"
	UsageAuxiliarySummary UsageScope = "auxiliary_summary"
	UsageOther            UsageScope = "other"
	UsageUnknown          UsageScope = "unknown"
)

// UsageMeasurement prevents consumers from summing cumulative counters as if
// every observation were an incremental charge.
type UsageMeasurement string

const (
	UsageDelta              UsageMeasurement = "delta"
	UsageCumulativeSnapshot UsageMeasurement = "cumulative_snapshot"
	UsageMeasurementUnknown UsageMeasurement = "unknown"
)

type UpdateMode string

const (
	UpdateDelta    UpdateMode = "delta"
	UpdateSnapshot UpdateMode = "snapshot"
)

type AssistantPhase string

const (
	AssistantCommentary   AssistantPhase = "commentary"
	AssistantFinal        AssistantPhase = "final"
	AssistantPhaseUnknown AssistantPhase = "unknown"
)

type ReasoningAvailability string

const (
	ReasoningAvailable   ReasoningAvailability = "available"
	ReasoningUnavailable ReasoningAvailability = "unavailable"
	ReasoningUnknown     ReasoningAvailability = "unknown"
)

type ToolOutcome string

const (
	ToolSucceeded ToolOutcome = "succeeded"
	ToolFailed    ToolOutcome = "failed"
	ToolCancelled ToolOutcome = "cancelled"
	ToolUnknown   ToolOutcome = "unknown"
)

type CommandStream string

const (
	CommandStdout        CommandStream = "stdout"
	CommandStderr        CommandStream = "stderr"
	CommandStreamUnknown CommandStream = "unknown"
)

type FileOperation string

const (
	FileRead    FileOperation = "read"
	FileCreate  FileOperation = "create"
	FileUpdate  FileOperation = "update"
	FileDelete  FileOperation = "delete"
	FileUnknown FileOperation = "unknown"
)

type PermissionScopeKind string

const (
	PermissionScopeFilesystem PermissionScopeKind = "filesystem"
	PermissionScopeProcess    PermissionScopeKind = "process"
	PermissionScopeNetwork    PermissionScopeKind = "network"
	PermissionScopeOther      PermissionScopeKind = "other"
)

type PendingRequestKind string

const (
	PendingPermission PendingRequestKind = "permission"
	PendingQuestion   PendingRequestKind = "question"
)

// CandidateSource identifies how candidate bytes entered the adapter. Valid JSON
// in commentary remains commentary and must not be silently promoted to a result.
type CandidateSource string

const (
	CandidateNativeStructured CandidateSource = "native_structured_output"
	CandidateConduitTool      CandidateSource = "conduit_result_tool"
	CandidateAssistantFinal   CandidateSource = "assistant_final"
	CandidateUnknown          CandidateSource = "unknown"
)
