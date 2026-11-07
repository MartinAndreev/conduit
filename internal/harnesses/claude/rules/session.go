package rules

// TokenSourceNone is the `initialize` account.tokenSource reported by Claude
// Code when the user has not logged in.
const TokenSourceNone = "none"

type AuthenticationKind string

const (
	AuthenticationPresent AuthenticationKind = "present"
	AuthenticationMissing AuthenticationKind = "missing"
	AuthenticationUnknown AuthenticationKind = "unknown"
)

type AuthenticationDecision struct {
	Kind   AuthenticationKind
	Reason string
}

// DecideAuthentication classifies `initialize` account.tokenSource. A missing
// login is a visible refusal, never a fallback. An absent field is unknown:
// the adapter cannot prove a login and must not invent its absence either.
func DecideAuthentication(tokenSource string) AuthenticationDecision {
	switch tokenSource {
	case TokenSourceNone:
		return AuthenticationDecision{
			Kind:   AuthenticationMissing,
			Reason: "claude reports no login (account.tokenSource is none)",
		}
	case "":
		return AuthenticationDecision{
			Kind:   AuthenticationUnknown,
			Reason: "claude did not report account.tokenSource",
		}
	}
	return AuthenticationDecision{
		Kind:   AuthenticationPresent,
		Reason: "claude reports a login token source",
	}
}

// StatusFacts describe the local session lifecycle when a native
// `system/status` observation arrives.
type StatusFacts struct {
	Closing bool
	// Ended is true when the local session is already lost or closed.
	Ended bool
}

type StatusKind string

const (
	StatusApply             StatusKind = "apply"
	StatusIgnoredWhileEnded StatusKind = "ignored_while_ended"
)

type StatusDecision struct {
	Kind   StatusKind
	Reason string
}

// DecideStatusTransition answers whether a native status may replace the local
// session state. A late status cannot reopen a closing, closed or lost session.
func DecideStatusTransition(f StatusFacts) StatusDecision {
	if f.Closing || f.Ended {
		return StatusDecision{
			Kind:   StatusIgnoredWhileEnded,
			Reason: "native status cannot overwrite a closing, closed or lost session",
		}
	}
	return StatusDecision{
		Kind:   StatusApply,
		Reason: "native status updates an open session",
	}
}

// ToolOutcomeFacts describe one native `tool_result` block.
type ToolOutcomeFacts struct {
	IsError bool
	// Interrupted is the Bash `tool_use_result.interrupted` flag.
	Interrupted bool
}

type ToolOutcomeKind string

const (
	ToolOutcomeSucceeded ToolOutcomeKind = "succeeded"
	ToolOutcomeFailed    ToolOutcomeKind = "failed"
	ToolOutcomeCancelled ToolOutcomeKind = "cancelled"
)

type ToolOutcomeDecision struct {
	Kind   ToolOutcomeKind
	Reason string
}

// DecideToolOutcome classifies a native tool result. An interrupted tool was
// stopped rather than failed on its own.
func DecideToolOutcome(f ToolOutcomeFacts) ToolOutcomeDecision {
	if f.Interrupted {
		return ToolOutcomeDecision{
			Kind:   ToolOutcomeCancelled,
			Reason: "native tool result reports an interruption",
		}
	}
	if f.IsError {
		return ToolOutcomeDecision{
			Kind:   ToolOutcomeFailed,
			Reason: "native tool result reports is_error",
		}
	}
	return ToolOutcomeDecision{
		Kind:   ToolOutcomeSucceeded,
		Reason: "native tool result reports no error",
	}
}
