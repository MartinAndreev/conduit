package rules

type ToolOutcomeKind string

const (
	ToolOutcomeSucceeded ToolOutcomeKind = "succeeded"
	ToolOutcomeFailed    ToolOutcomeKind = "failed"
	ToolOutcomeCancelled ToolOutcomeKind = "cancelled"
	ToolOutcomeUnknown   ToolOutcomeKind = "unknown"
)

// AbortedToolError is the error text OpenCode records on a tool part that was
// stopped while in flight.
const AbortedToolError = "Tool execution aborted"

type ToolOutcomeFacts struct {
	// NativeStatus is the tool part's native `state.status`.
	NativeStatus string
	// CommandTool reports that the native tool runs a shell command.
	CommandTool bool
	// ExitKnown and ExitCode carry `state.metadata.exit` when present.
	ExitKnown bool
	ExitCode  int
	// ErrorText is the native `state.error` text of an errored part.
	ErrorText string
	// Interrupted is the native `state.metadata.interrupted` flag.
	Interrupted bool
}

type ToolOutcomeDecision struct {
	Kind   ToolOutcomeKind
	Reason string
}

// DecideToolOutcome classifies one native tool part. OpenCode marks a shell tool
// `completed` even when its command exits nonzero, so a known nonzero exit
// overrides the native completion. An errored part that was stopped in flight
// is a cancellation rather than a tool failure.
func DecideToolOutcome(f ToolOutcomeFacts) ToolOutcomeDecision {
	switch f.NativeStatus {
	case "completed":
		if f.CommandTool && f.ExitKnown && f.ExitCode != 0 {
			return ToolOutcomeDecision{
				Kind:   ToolOutcomeFailed,
				Reason: "OpenCode completed the shell tool but its command exited nonzero",
			}
		}
		return ToolOutcomeDecision{
			Kind:   ToolOutcomeSucceeded,
			Reason: "OpenCode reported the tool completed",
		}
	case "error", "failed":
		if f.Interrupted || f.ErrorText == AbortedToolError {
			return ToolOutcomeDecision{
				Kind:   ToolOutcomeCancelled,
				Reason: "OpenCode aborted the tool while it was running",
			}
		}
		return ToolOutcomeDecision{
			Kind:   ToolOutcomeFailed,
			Reason: "OpenCode reported a tool error",
		}
	case "cancelled":
		return ToolOutcomeDecision{
			Kind:   ToolOutcomeCancelled,
			Reason: "OpenCode reported the tool cancelled",
		}
	}
	return ToolOutcomeDecision{
		Kind:   ToolOutcomeUnknown,
		Reason: "OpenCode tool status is not terminal or not recognized",
	}
}
