package provider

import "fmt"

type ErrorCode string

const (
	ErrorCapabilityUnsupported ErrorCode = "capability_unsupported"
	ErrorCapabilityUnknown     ErrorCode = "capability_unknown"
	ErrorStaleBinding          ErrorCode = "stale_binding"
	ErrorNativeTargetRejected  ErrorCode = "native_target_rejected"
	ErrorDeliveryRejected      ErrorCode = "delivery_rejected"
	ErrorDeliveryUnknown       ErrorCode = "delivery_unknown"
	ErrorSessionLost           ErrorCode = "session_lost"
	ErrorTurnLost              ErrorCode = "turn_lost"
	ErrorProtocolViolation     ErrorCode = "protocol_violation"
	ErrorEventGap              ErrorCode = "event_gap"
	ErrorBackpressureExceeded  ErrorCode = "backpressure_exceeded"
	ErrorAuthFailed            ErrorCode = "auth_failed"
	ErrorExecutableMissing     ErrorCode = "executable_missing"
	ErrorDeadline              ErrorCode = "deadline"
	ErrorCallerCancelled       ErrorCode = "caller_cancelled"
)

type Operation string

const (
	OperationInspect           Operation = "inspect"
	OperationOpenSession       Operation = "open_session"
	OperationResumeSession     Operation = "resume_session"
	OperationSubscribeEvents   Operation = "subscribe_events"
	OperationReceiveEvent      Operation = "receive_event"
	OperationObserveState      Operation = "observe_state"
	OperationStartTurn         Operation = "start_turn"
	OperationSteer             Operation = "steer"
	OperationRespondPermission Operation = "respond_permission"
	OperationAnswerQuestion    Operation = "answer_question"
	OperationInterruptTurn     Operation = "interrupt_turn"
	OperationCloseSession      Operation = "close_session"
)

type RetryClass string

const (
	RetryNever          RetryClass = "never"
	RetrySafe           RetryClass = "safe"
	RetryAfterReconcile RetryClass = "after_reconcile"
	RetryUnknown        RetryClass = "unknown"
)

type RemoteEffect string

const (
	RemoteEffectNotSent      RemoteEffect = "not_sent"
	RemoteEffectPossiblySent RemoteEffect = "possibly_sent"
	RemoteEffectAccepted     RemoteEffect = "accepted"
	RemoteEffectUnknown      RemoteEffect = "unknown"
)

// ProviderError preserves structured transport failure semantics. Error omits
// Cause text deliberately so logs do not accidentally expose native diagnostics;
// callers can inspect the wrapped cause with errors.Is/errors.As.
type ProviderError struct {
	Code         ErrorCode
	Operation    Operation
	Session      Optional[SessionRef]
	Turn         Optional[TurnRef]
	RequestID    Optional[RequestID]
	Retry        RetryClass
	RemoteEffect RemoteEffect
	Detail       string // bounded, caller-safe detail; never raw native diagnostics
	Cause        error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Detail != "" {
		return fmt.Sprintf("provider %s failed: %s (%s)", e.Operation, e.Code, e.Detail)
	}
	return fmt.Sprintf("provider %s failed: %s", e.Operation, e.Code)
}

// Unwrap exposes the machine-usable cause without including it in Error output.
func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is supports errors.Is matching by code and, when provided by the target,
// operation. Empty target fields act as wildcards.
func (e *ProviderError) Is(target error) bool {
	want, ok := target.(*ProviderError)
	if !ok || e == nil || want == nil {
		return false
	}
	if want.Code != "" && e.Code != want.Code {
		return false
	}
	return want.Operation == "" || e.Operation == want.Operation
}
