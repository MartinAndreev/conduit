// Package rules owns startup adapter metadata policies, without process effects.
package rules

type ProbeKind string

const (
	CleanupUnconfirmed ProbeKind = "cleanup_unconfirmed"
	AcceptMetadata     ProbeKind = "accept_metadata"
	RejectMetadata     ProbeKind = "reject_metadata"
	MetadataTimedOut   ProbeKind = "metadata_timed_out"
	MetadataCancelled  ProbeKind = "metadata_cancelled"
)

type ProbeFacts struct {
	Cancelled        bool
	TimedOut         bool
	CleanupFailed    bool
	ProcessSucceeded bool
	CaptureExceeded  bool
}
type ProbeDecision struct {
	Kind   ProbeKind
	Reason string
}

// User cancellation takes precedence over a simultaneous timeout. Failed cleanup
// cannot be presented as an ordinary successful bounded timeout.
func DecideProbe(f ProbeFacts) ProbeDecision {
	if f.Cancelled {
		return ProbeDecision{MetadataCancelled, "The user cancelled metadata discovery."}
	}
	if f.CleanupFailed {
		return ProbeDecision{CleanupUnconfirmed, "Owned process cleanup could not be confirmed."}
	}
	if f.TimedOut {
		return ProbeDecision{MetadataTimedOut, "The local metadata probe exceeded its deadline."}
	}
	if !f.ProcessSucceeded || f.CaptureExceeded {
		return ProbeDecision{RejectMetadata, "Metadata did not complete within the accepted output limits."}
	}
	return ProbeDecision{AcceptMetadata, "Local version output is eligible for parsing; readiness capabilities remain unknown."}
}
