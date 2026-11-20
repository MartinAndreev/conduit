package rules

// ReleaseFacts describe one tracked release of an owned process that the
// session stopped waiting for: a run client or control command whose
// finalization was not confirmed in time, or a cancelled control command's
// cleanup owner.
type ReleaseFacts struct {
	// Finished is true once the release work ended (its observer's Done
	// closed); the other facts are final only then.
	Finished bool
	// RootReaped is true when finalization released (reaped) the root.
	RootReaped bool
	// ReleaseFailed is true when finalization ended with an error (a failed
	// reap or an unavailable release).
	ReleaseFailed bool
	// PipeCloseFailed is true when closing the parent's pipe ends failed.
	PipeCloseFailed bool
}

type ReleaseKind string

const (
	// ReleaseConfirmed: the root was reaped and the pipes closed without
	// error; the release no longer blocks ResourcesReleased.
	ReleaseConfirmed ReleaseKind = "confirmed"
	// ReleasePending: the release is still running; it may still confirm.
	ReleasePending ReleaseKind = "pending"
	// ReleaseFailed: the release ended without confirmation; it blocks
	// ResourcesReleased for the rest of the session.
	ReleaseFailed ReleaseKind = "failed"
)

type ReleaseDecision struct {
	Kind   ReleaseKind
	Reason string
}

// Confirmed reports whether the release no longer blocks ResourcesReleased.
func (d ReleaseDecision) Confirmed() bool { return d.Kind == ReleaseConfirmed }

// DecideRelease answers whether a tracked process release is confirmed.
// Only a finished release with the root reaped and no release or pipe-close
// error is confirmed; a running one is pending and a finished one without
// that proof failed. Both keep ResourcesReleased false. A refused tree
// termination of a cancelled control command is reported in its error and
// is not a release fact.
func DecideRelease(f ReleaseFacts) ReleaseDecision {
	switch {
	case !f.Finished:
		return ReleaseDecision{Kind: ReleasePending, Reason: "an owned OpenCode client process is still being released"}
	case f.ReleaseFailed || f.PipeCloseFailed || !f.RootReaped:
		return ReleaseDecision{Kind: ReleaseFailed, Reason: "an owned OpenCode client process release ended without a confirmed reap and pipe close"}
	default:
		return ReleaseDecision{Kind: ReleaseConfirmed, Reason: "the owned OpenCode client process was reaped and its pipes closed"}
	}
}
