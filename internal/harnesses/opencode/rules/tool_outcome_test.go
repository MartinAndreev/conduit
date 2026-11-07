package rules

import "testing"

func TestDecideToolOutcome(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts ToolOutcomeFacts
		want  ToolOutcomeKind
	}{
		{"completed tool", ToolOutcomeFacts{NativeStatus: "completed"}, ToolOutcomeSucceeded},
		{"shell exit zero", ToolOutcomeFacts{NativeStatus: "completed", CommandTool: true, ExitKnown: true}, ToolOutcomeSucceeded},
		{"shell exit unknown", ToolOutcomeFacts{NativeStatus: "completed", CommandTool: true}, ToolOutcomeSucceeded},
		{"shell exit nonzero", ToolOutcomeFacts{NativeStatus: "completed", CommandTool: true, ExitKnown: true, ExitCode: 2}, ToolOutcomeFailed},
		{"non-shell exit ignored", ToolOutcomeFacts{NativeStatus: "completed", ExitKnown: true, ExitCode: 2}, ToolOutcomeSucceeded},
		{"error", ToolOutcomeFacts{NativeStatus: "error", ErrorText: "boom"}, ToolOutcomeFailed},
		{"failed", ToolOutcomeFacts{NativeStatus: "failed"}, ToolOutcomeFailed},
		{"aborted text", ToolOutcomeFacts{NativeStatus: "error", ErrorText: AbortedToolError}, ToolOutcomeCancelled},
		{"interrupted flag", ToolOutcomeFacts{NativeStatus: "error", ErrorText: "stopped", Interrupted: true}, ToolOutcomeCancelled},
		{"cancelled", ToolOutcomeFacts{NativeStatus: "cancelled"}, ToolOutcomeCancelled},
		{"running", ToolOutcomeFacts{NativeStatus: "running"}, ToolOutcomeUnknown},
	} {
		d := DecideToolOutcome(tc.facts)
		if d.Kind != tc.want || d.Reason == "" {
			t.Fatalf("%s: decision=%#v want %s", tc.name, d, tc.want)
		}
	}
}
