// Package rules holds the technical policy decisions of the shared Agent Client
// Protocol (ACP) client. Rules are pure: they take explicit facts and return a
// typed decision. They import nothing from the parent adapter package.
package rules

// ResultEnforcementKind describes how a result contract can be honoured. ACP
// has no native schema field, so only an explicit "none" fallback is allowed.
type ResultEnforcementKind string

const (
	ResultEnforcementNone          ResultEnforcementKind = "none"
	ResultEnforcementInvalidSchema ResultEnforcementKind = "invalid_schema"
	ResultEnforcementUnsupported   ResultEnforcementKind = "unsupported"
)

type ResultEnforcementFacts struct {
	SchemaPresent   bool
	SchemaValidJSON bool
	AllowsNone      bool
}

type ResultEnforcementDecision struct {
	Kind   ResultEnforcementKind
	Reason string
}

// DecideResultEnforcement gives schema validity precedence over support.
func DecideResultEnforcement(f ResultEnforcementFacts) ResultEnforcementDecision {
	if f.SchemaPresent && !f.SchemaValidJSON {
		return ResultEnforcementDecision{
			Kind:   ResultEnforcementInvalidSchema,
			Reason: "result schema is not valid JSON",
		}
	}
	if f.AllowsNone {
		return ResultEnforcementDecision{
			Kind:   ResultEnforcementNone,
			Reason: "result contract permits no native schema enforcement",
		}
	}
	return ResultEnforcementDecision{
		Kind:   ResultEnforcementUnsupported,
		Reason: "ACP has no native result schema",
	}
}

// StartTurnKind describes turn-start eligibility.
type StartTurnKind string

const (
	StartTurnAllowed        StartTurnKind = "allowed"
	StartTurnBindingDiffers StartTurnKind = "binding_differs"
	StartTurnUnavailable    StartTurnKind = "unavailable"
	StartTurnOccupied       StartTurnKind = "occupied"
)

type StartTurnFacts struct {
	SessionMatches            bool
	AssignmentIdentityMatches bool
	Closing                   bool
	SessionLost               bool
	// TurnUnresolved is true while a session/prompt request has no response
	// yet, including one whose caller stopped waiting.
	TurnUnresolved bool
}

type StartTurnDecision struct {
	Kind   StartTurnKind
	Reason string
}

// DecideStartTurn permits one session/prompt at a time. A prompt whose
// response has not arrived still occupies the session: ACP ends a turn only
// with that response.
func DecideStartTurn(f StartTurnFacts) StartTurnDecision {
	if !f.SessionMatches || !f.AssignmentIdentityMatches {
		return StartTurnDecision{
			Kind:   StartTurnBindingDiffers,
			Reason: "session or assignment binding differs",
		}
	}
	if f.Closing || f.SessionLost {
		return StartTurnDecision{
			Kind:   StartTurnUnavailable,
			Reason: "session is closing, closed or lost",
		}
	}
	if f.TurnUnresolved {
		return StartTurnDecision{
			Kind:   StartTurnOccupied,
			Reason: "a session/prompt request is still unresolved",
		}
	}
	return StartTurnDecision{
		Kind:   StartTurnAllowed,
		Reason: "session is eligible to send session/prompt",
	}
}

// PromptOutcomeKind classifies the session/prompt response, which is the
// native turn terminal.
type PromptOutcomeKind string

const (
	PromptCompleted            PromptOutcomeKind = "completed"
	PromptCompletedWithWarning PromptOutcomeKind = "completed_with_warning"
	PromptCancelled            PromptOutcomeKind = "cancelled"
	PromptFailed               PromptOutcomeKind = "failed"
	// PromptMalformed: a response arrived, so the native turn is over, but its
	// result could not be decoded.
	PromptMalformed PromptOutcomeKind = "malformed"
	// PromptTransportLost: the transport stopped before any response; the
	// native turn outcome is unknown.
	PromptTransportLost PromptOutcomeKind = "transport_lost"
)

type PromptOutcomeFacts struct {
	TransportStopped bool
	NativeError      bool
	ResultDecoded    bool
	StopReason       string
}

type PromptOutcomeDecision struct {
	Kind   PromptOutcomeKind
	Reason string
}

// DecidePromptOutcome maps the ACP stopReason. Transport loss precedes a
// native error because no native response was observed.
func DecidePromptOutcome(f PromptOutcomeFacts) PromptOutcomeDecision {
	if f.TransportStopped {
		return PromptOutcomeDecision{
			Kind:   PromptTransportLost,
			Reason: "transport stopped before the session/prompt response",
		}
	}
	if f.NativeError {
		return PromptOutcomeDecision{
			Kind:   PromptFailed,
			Reason: "session/prompt returned a JSON-RPC error",
		}
	}
	if !f.ResultDecoded {
		return PromptOutcomeDecision{
			Kind:   PromptMalformed,
			Reason: "session/prompt response could not be decoded",
		}
	}
	switch f.StopReason {
	case "end_turn":
		return PromptOutcomeDecision{
			Kind:   PromptCompleted,
			Reason: "agent ended the turn",
		}
	case "cancelled":
		return PromptOutcomeDecision{
			Kind:   PromptCancelled,
			Reason: "agent reported the turn cancelled",
		}
	case "max_tokens", "refusal", "max_turn_requests":
		return PromptOutcomeDecision{
			Kind:   PromptCompletedWithWarning,
			Reason: "agent stopped the turn early: " + f.StopReason,
		}
	default:
		return PromptOutcomeDecision{
			Kind:   PromptCompletedWithWarning,
			Reason: "agent returned an unrecognized stop reason",
		}
	}
}

// InterruptKind describes whether session/cancel may be sent.
type InterruptKind string

const (
	InterruptAllowed        InterruptKind = "allowed"
	InterruptBindingDiffers InterruptKind = "binding_differs"
	InterruptNoActiveTurn   InterruptKind = "no_active_turn"
)

type InterruptFacts struct {
	SessionMatches    bool
	ActiveTurn        bool
	ActiveTurnMatches bool
	AssignmentMatches bool
}

type InterruptDecision struct {
	Kind   InterruptKind
	Reason string
}

// DecideInterrupt targets session/cancel only at the turn the caller expects.
func DecideInterrupt(f InterruptFacts) InterruptDecision {
	if !f.SessionMatches || !f.AssignmentMatches {
		return InterruptDecision{
			Kind:   InterruptBindingDiffers,
			Reason: "session or assignment binding differs",
		}
	}
	if !f.ActiveTurn {
		return InterruptDecision{
			Kind:   InterruptNoActiveTurn,
			Reason: "no session/prompt is unresolved",
		}
	}
	if !f.ActiveTurnMatches {
		return InterruptDecision{
			Kind:   InterruptBindingDiffers,
			Reason: "interrupt target differs from the active turn",
		}
	}
	return InterruptDecision{
		Kind:   InterruptAllowed,
		Reason: "session/cancel may be sent for the active turn",
	}
}
