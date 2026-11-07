package rules

import "strings"

// Native terminal reasons that Claude Code reports for an intermediate result
// while a steer (`priority:"now"`) message preempts the running request. Such a
// result is followed by another result for the same turn.
const (
	TerminalReasonAbortedStreaming = "aborted_streaming"
	TerminalReasonAbortedTools     = "aborted_tools"
)

// ResultTerminalFacts describe one native `result` message against the local
// active turn. IntersectsActiveInputs is true when any of the result's
// `user_message_uuid`/`user_message_uuids` belongs to the active turn's input
// set (the initial message UUID and every dispatched steer UUID).
type ResultTerminalFacts struct {
	ActiveTurn             bool
	IntersectsActiveInputs bool
	TerminalReason         string
}

type ResultTerminalKind string

const (
	ResultTerminal     ResultTerminalKind = "terminal"
	ResultIntermediate ResultTerminalKind = "intermediate"
	ResultForeign      ResultTerminalKind = "foreign"
)

type ResultTerminalDecision struct {
	Kind   ResultTerminalKind
	Reason string
}

// DecideResultTerminal answers whether a native result ends the active turn.
// Correlation precedes the terminal reason: a result for foreign input UUIDs
// never clears the active turn, even when its reason would be final.
func DecideResultTerminal(f ResultTerminalFacts) ResultTerminalDecision {
	if !f.ActiveTurn || !f.IntersectsActiveInputs {
		return ResultTerminalDecision{
			Kind:   ResultForeign,
			Reason: "result does not reference an input of the active turn",
		}
	}
	if f.TerminalReason == TerminalReasonAbortedStreaming || f.TerminalReason == TerminalReasonAbortedTools {
		return ResultTerminalDecision{
			Kind:   ResultIntermediate,
			Reason: "result was aborted by a steer and the turn continues",
		}
	}
	return ResultTerminalDecision{
		Kind:   ResultTerminal,
		Reason: "result references the active turn and ends it",
	}
}

// ResultOutcomeFacts carry the native `result.subtype` and `result.is_error`.
type ResultOutcomeFacts struct {
	Subtype string
	IsError bool
}

type ResultOutcomeKind string

const (
	ResultSucceeded ResultOutcomeKind = "succeeded"
	ResultFailed    ResultOutcomeKind = "failed"
)

type ResultOutcomeDecision struct {
	Kind   ResultOutcomeKind
	Reason string
}

// DecideResultOutcome classifies a native result. Claude Code reports some
// failures (for example a missing login) as `subtype:"success"` with
// `is_error:true`, so the flag takes precedence over the subtype; every
// `error_*` subtype is a failure as well.
func DecideResultOutcome(f ResultOutcomeFacts) ResultOutcomeDecision {
	if f.IsError {
		return ResultOutcomeDecision{
			Kind:   ResultFailed,
			Reason: "native result reports is_error",
		}
	}
	if strings.HasPrefix(f.Subtype, "error_") {
		return ResultOutcomeDecision{
			Kind:   ResultFailed,
			Reason: "native result subtype is an error",
		}
	}
	return ResultOutcomeDecision{
		Kind:   ResultSucceeded,
		Reason: "native result reports no error",
	}
}
