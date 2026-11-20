package rules

// ReleaseFacts describe one release of an owned OpenCode process (the
// session's own server, a run client, or a control command).
type ReleaseFacts struct {
	// Finished is true once the release work ended (its observer's Done
	// closed); the other facts are final only then.
	Finished bool
	// RootReaped is true when finalization released (reaped) the root.
	RootReaped bool
	// ReleaseFailed is true when Wait or finalization ended with a release
	// error (for example a failed reap, an unavailable release, or a failed
	// native handle/job close).
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

// DecideRelease answers whether an owned process release is confirmed.
// Only a finished release with the root reaped and no release or pipe-close
// error is confirmed; a running one is pending and a finished one without
// that proof failed. Both keep ResourcesReleased false.
func DecideRelease(f ReleaseFacts) ReleaseDecision {
	switch {
	case !f.Finished:
		return ReleaseDecision{Kind: ReleasePending, Reason: "an owned OpenCode process is still being released"}
	case f.ReleaseFailed || f.PipeCloseFailed || !f.RootReaped:
		return ReleaseDecision{Kind: ReleaseFailed, Reason: "an owned OpenCode process release ended without a confirmed reap and pipe close"}
	default:
		return ReleaseDecision{Kind: ReleaseConfirmed, Reason: "the owned OpenCode process was reaped and its pipes closed"}
	}
}

// ClientOwner records who reserved the termination and finalization of one
// run client under the session mutex. Every run client exit — clean or failed
// — terminates its own process group before finalization.
type ClientOwner string

const (
	// ClientOwnerNone: no path has reserved termination yet.
	ClientOwnerNone ClientOwner = ""
	// ClientOwnerKilled: a non-Close path (run observation on clean or failed
	// exit, server loss, or stdin-close failure) reserved the client's single
	// group kill and post-kill finalization.
	ClientOwnerKilled ClientOwner = "killed"
	// ClientOwnerClose: Close reserved the client's single group kill and
	// post-kill finalization.
	ClientOwnerClose ClientOwner = "close"
)

// ClientClaimActor names the path asking to claim or join a run client's
// termination and finalization.
type ClientClaimActor string

const (
	// ClientActorRun is the run's observation (watchRunRootExit / readRun /
	// killRunClient). It yields if Close already claimed the client so it
	// cannot finalize ahead of Close's kill.
	ClientActorRun ClientClaimActor = "run"
	// ClientActorKill is an external non-Close termination path (watchServer
	// or stdin-close failure cleanup).
	ClientActorKill ClientClaimActor = "kill"
	// ClientActorClose is session.Close.
	ClientActorClose ClientClaimActor = "close"
)

// ClientClaimFacts are the current reservation of one run client and the
// actor requesting to terminate and finalize it.
type ClientClaimFacts struct {
	Current ClientOwner
	Actor   ClientClaimActor
}

type ClientClaimKind string

const (
	// ClientClaimKill: the actor reserved the client's single group kill and
	// post-kill finalization.
	ClientClaimKill ClientClaimKind = "kill"
	// ClientClaimAwaitKill: a termination path already reserved the client;
	// share its single kill worker and release observer.
	ClientClaimAwaitKill ClientClaimKind = "await_kill"
	// ClientClaimYieldToClose: Close already claimed the client's
	// termination, so readRun must yield instead of finalizing ahead of
	// Close's kill.
	ClientClaimYieldToClose ClientClaimKind = "yield_to_close"
	// ClientClaimInvalid: Current or Actor is not a known value.
	ClientClaimInvalid ClientClaimKind = "invalid"
)

type ClientClaimDecision struct {
	Kind      ClientClaimKind
	NextOwner ClientOwner
	Reason    string
}

// StartsKill reports whether the actor should ensure the client's single kill
// worker is started.
func (d ClientClaimDecision) StartsKill() bool {
	return d.Kind == ClientClaimKill || d.Kind == ClientClaimAwaitKill
}

// DecideClientClaim decides atomically (under the session mutex) who owns a
// run client's single group kill and finalization. Every exit terminates the
// run client's process group before finalization; once Close claims the
// client, readRun yields so it cannot finalize ahead of Close's kill. Invalid
// owner or actor values are rejected explicitly without mutating ownership.
func DecideClientClaim(f ClientClaimFacts) ClientClaimDecision {
	switch f.Current {
	case ClientOwnerNone, ClientOwnerKilled, ClientOwnerClose:
	default:
		return ClientClaimDecision{Kind: ClientClaimInvalid, NextOwner: f.Current, Reason: "unknown run client owner state"}
	}
	switch f.Actor {
	case ClientActorRun:
		switch f.Current {
		case ClientOwnerNone:
			return ClientClaimDecision{Kind: ClientClaimKill, NextOwner: ClientOwnerKilled, Reason: "run observation reserved the run client's single group kill and finalization"}
		case ClientOwnerKilled:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerKilled, Reason: "a termination kill was already reserved; share its single kill and release"}
		case ClientOwnerClose:
			return ClientClaimDecision{Kind: ClientClaimYieldToClose, NextOwner: ClientOwnerClose, Reason: "Close already claimed the run client; yield termination and finalization to Close"}
		}
	case ClientActorKill:
		switch f.Current {
		case ClientOwnerNone:
			return ClientClaimDecision{Kind: ClientClaimKill, NextOwner: ClientOwnerKilled, Reason: "termination path reserved the run client's single group kill and finalization"}
		case ClientOwnerKilled:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerKilled, Reason: "the run client's single group kill was already reserved; share its outcome"}
		case ClientOwnerClose:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerClose, Reason: "Close already reserved the run client's single group kill; share its outcome"}
		}
	case ClientActorClose:
		switch f.Current {
		case ClientOwnerNone:
			return ClientClaimDecision{Kind: ClientClaimKill, NextOwner: ClientOwnerClose, Reason: "Close reserved the run client's single group kill and finalization"}
		case ClientOwnerKilled:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerKilled, Reason: "a termination kill was already reserved; Close shares its outcome and release"}
		case ClientOwnerClose:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerClose, Reason: "Close already reserved the run client; repeated Close shares the same kill and release"}
		}
	}
	return ClientClaimDecision{Kind: ClientClaimInvalid, NextOwner: f.Current, Reason: "unknown run client claim actor"}
}

// ClientCloseReleaseFacts describe a run client's termination and release.
// The same facts and rule apply whether the client is still active in
// s.client or tracked in s.releases.
type ClientCloseReleaseFacts struct {
	// KillFinished is true when the client's single group kill completed
	// within the caller's wait; KillConfirmed is final only then.
	KillFinished bool
	// KillConfirmed is true when the client's single group kill confirmed
	// termination (process.KillConfirmed). ErrSignalsFinalized is never
	// confirmed.
	KillConfirmed bool
	// TreeKillSupported is true when KillTree terminates the whole owned
	// process group rather than only the root.
	TreeKillSupported bool
	// Release are the facts of the shared Wait + Finalize + pipe-close
	// release observer.
	Release ReleaseFacts
}

// DecideClientCloseRelease answers whether a run client is confirmed stopped
// and released for ResourcesReleased accounting. Confirmation requires the
// single group kill to have finished and confirmed termination, tree-kill
// support, and DecideRelease confirmation of Wait, reap, stdin, and forced
// pipe closure.
func DecideClientCloseRelease(f ClientCloseReleaseFacts) ReleaseDecision {
	if !f.KillFinished {
		return ReleaseDecision{Kind: ReleasePending, Reason: "the owned OpenCode run client's group termination is still running"}
	}
	if !f.KillConfirmed {
		return ReleaseDecision{Kind: ReleaseFailed, Reason: "the owned OpenCode run client's group termination was not confirmed"}
	}
	if !f.TreeKillSupported {
		return ReleaseDecision{Kind: ReleaseFailed, Reason: "only the owned OpenCode run client root could be terminated; a descendant may still run"}
	}
	return DecideRelease(f.Release)
}

// CancelledControlReleaseFacts describe the cleanup of a control command
// whose caller stopped waiting and initiated process-group termination.
type CancelledControlReleaseFacts struct {
	// TreeKillFailed is true when KillTree did not confirm termination (OS
	// refusal or ErrSignalsFinalized without a prior confirmed kill).
	TreeKillFailed bool
	// TreeKillSupported is true when KillTree terminates the whole owned
	// process group rather than only the root.
	TreeKillSupported bool
	// Release are the cleanup owner's release facts (Complete, RootReaped,
	// Wait/Finalize ReleaseErr, PipeCloseErr, StdinCloseErr).
	Release ReleaseFacts
}

// DecideCancelledControlRelease answers whether a cancelled control command's
// cleanup confirms that its process group was stopped and its resources
// released. A refused group kill or unsupported tree kill blocks
// ResourcesReleased even after the root and pipes eventually finish.
func DecideCancelledControlRelease(f CancelledControlReleaseFacts) ReleaseDecision {
	if !f.Release.Finished {
		return ReleaseDecision{Kind: ReleasePending, Reason: "a cancelled OpenCode control command is still being cleaned up"}
	}
	if f.TreeKillFailed {
		return ReleaseDecision{Kind: ReleaseFailed, Reason: "the cancelled OpenCode control command's group termination was not confirmed"}
	}
	if !f.TreeKillSupported {
		return ReleaseDecision{Kind: ReleaseFailed, Reason: "only the cancelled OpenCode control command root could be terminated; a descendant may still run"}
	}
	return DecideRelease(f.Release)
}
