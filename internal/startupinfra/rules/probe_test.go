package rules

import "testing"

func TestProbePolicy(t *testing.T) {
	for _, tc := range []struct {
		facts ProbeFacts
		want  ProbeKind
	}{{ProbeFacts{Cancelled: true, TimedOut: true, CleanupFailed: true}, MetadataCancelled}, {ProbeFacts{TimedOut: true, CleanupFailed: true}, CleanupUnconfirmed}, {ProbeFacts{TimedOut: true}, MetadataTimedOut}, {ProbeFacts{}, RejectMetadata}, {ProbeFacts{ProcessSucceeded: true, CaptureExceeded: true}, RejectMetadata}, {ProbeFacts{ProcessSucceeded: true}, AcceptMetadata}} {
		d := DecideProbe(tc.facts)
		if d.Kind != tc.want || d.Reason == "" {
			t.Fatal(d)
		}
	}
}
