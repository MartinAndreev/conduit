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
// that proof failed. Both keep ResourcesReleased false. A refused tree
// termination of a cancelled control command is reported in its error and
// is not a release fact.
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

// ClientOwner records who reserved the finalization path of one run client
// under the session mutex.
type ClientOwner string

const (
	// ClientOwnerNone: no path has reserved finalization yet.
	ClientOwnerNone ClientOwner = ""
	// ClientOwnerNatural: readRun observed a clean end and reserved
	// finalization without a group kill.
	ClientOwnerNatural ClientOwner = "natural"
	// ClientOwnerKilled: a non-Close termination path (failed run, server
	// loss, or stdin-close failure) reserved the client's single group kill
	// and post-kill finalization.
	ClientOwnerKilled ClientOwner = "killed"
	// ClientOwnerClose: Close reserved the client's single group kill and
	// post-kill finalization.
	ClientOwnerClose ClientOwner = "close"
)

// ClientClaimActor names the path asking to claim or join a run client's
// termination and finalization.
type ClientClaimActor string

const (
	ClientActorNatural ClientClaimActor = "natural"
	ClientActorKill    ClientClaimActor = "kill"
	ClientActorClose   ClientClaimActor = "close"
)

// ClientClaimFacts are the current reservation of one run client and the
// actor requesting to finalize or terminate it.
type ClientClaimFacts struct {
	Current ClientOwner
	Actor   ClientClaimActor
}

type ClientClaimKind string

const (
	// ClientClaimFinalizeNaturally: the actor (readRun) reserved natural
	// finalization without a group kill.
	ClientClaimFinalizeNaturally ClientClaimKind = "finalize_naturally"
	// ClientClaimKill: the actor reserved the client's single group kill and
	// post-kill finalization.
	ClientClaimKill ClientClaimKind = "kill"
	// ClientClaimAwaitNatural: natural finalization was already reserved; do
	// not send a kill, await the shared release observer instead.
	ClientClaimAwaitNatural ClientClaimKind = "await_natural"
	// ClientClaimAwaitKill: a kill path already reserved the client; share
	// its single kill and post-kill release observer.
	ClientClaimAwaitKill ClientClaimKind = "await_kill"
	// ClientClaimYieldToClose: Close already claimed the client's
	// termination, so readRun must not finalize it ahead of Close's kill.
	ClientClaimYieldToClose ClientClaimKind = "yield_to_close"
	// ClientClaimInvalid: Current or Actor is not a known value.
	ClientClaimInvalid ClientClaimKind = "invalid"
)

type ClientClaimDecision struct {
	Kind      ClientClaimKind
	NextOwner ClientOwner
	Reason    string
}

// NeedsKill reports whether the client's release path includes its single
// group kill (either reserved now or previously by a kill/Close path).
func (d ClientClaimDecision) NeedsKill() bool {
	return d.Kind == ClientClaimKill || d.Kind == ClientClaimAwaitKill
}

// NaturalRelease reports whether the client follows natural finalization
// without a group kill.
func (d ClientClaimDecision) NaturalRelease() bool {
	return d.Kind == ClientClaimFinalizeNaturally || d.Kind == ClientClaimAwaitNatural
}

// DecideClientClaim decides atomically (under the session mutex) who owns a
// run client's finalization and whether a group kill is sent. Once natural
// finalization is reserved, later Close or loss paths await that natural
// release instead of sending a kill that finalization would refuse. Once
// Close claims termination, readRun yields so it cannot finalize ahead of
// Close's kill. Invalid owner or actor values are rejected explicitly without
// mutating ownership.
func DecideClientClaim(f ClientClaimFacts) ClientClaimDecision {
	switch f.Current {
	case ClientOwnerNone, ClientOwnerNatural, ClientOwnerKilled, ClientOwnerClose:
	default:
		return ClientClaimDecision{Kind: ClientClaimInvalid, NextOwner: f.Current, Reason: "unknown run client owner state"}
	}
	switch f.Actor {
	case ClientActorNatural:
		switch f.Current {
		case ClientOwnerNone:
			return ClientClaimDecision{Kind: ClientClaimFinalizeNaturally, NextOwner: ClientOwnerNatural, Reason: "run observation reserved natural finalization without a group kill"}
		case ClientOwnerNatural:
			return ClientClaimDecision{Kind: ClientClaimAwaitNatural, NextOwner: ClientOwnerNatural, Reason: "natural finalization was already reserved; await the shared release"}
		case ClientOwnerKilled:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerKilled, Reason: "a termination kill was already reserved; await the shared post-kill release"}
		case ClientOwnerClose:
			return ClientClaimDecision{Kind: ClientClaimYieldToClose, NextOwner: ClientOwnerClose, Reason: "Close already claimed the run client; yield finalization to Close"}
		}
	case ClientActorKill:
		switch f.Current {
		case ClientOwnerNone:
			return ClientClaimDecision{Kind: ClientClaimKill, NextOwner: ClientOwnerKilled, Reason: "termination path reserved the run client's single group kill and finalization"}
		case ClientOwnerNatural:
			return ClientClaimDecision{Kind: ClientClaimAwaitNatural, NextOwner: ClientOwnerNatural, Reason: "run observation already reserved natural finalization; do not kill"}
		case ClientOwnerKilled:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerKilled, Reason: "the run client's single group kill was already reserved; share its outcome"}
		case ClientOwnerClose:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerClose, Reason: "Close already reserved the run client's single group kill; share its outcome"}
		}
	case ClientActorClose:
		switch f.Current {
		case ClientOwnerNone:
			return ClientClaimDecision{Kind: ClientClaimKill, NextOwner: ClientOwnerClose, Reason: "Close reserved the run client's single group kill and finalization"}
		case ClientOwnerNatural:
			return ClientClaimDecision{Kind: ClientClaimAwaitNatural, NextOwner: ClientOwnerNatural, Reason: "run observation already reserved natural finalization; Close awaits that release without killing"}
		case ClientOwnerKilled:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerKilled, Reason: "a termination kill was already reserved; Close awaits its outcome and shared release"}
		case ClientOwnerClose:
			return ClientClaimDecision{Kind: ClientClaimAwaitKill, NextOwner: ClientOwnerClose, Reason: "Close already reserved the run client; repeated Close awaits the same kill and shared release"}
		}
	}
	return ClientClaimDecision{Kind: ClientClaimInvalid, NextOwner: f.Current, Reason: "unknown run client claim actor"}
}

// ClientCloseReleaseFacts describe a run client's termination and release as
// evaluated by Close.
type ClientCloseReleaseFacts struct {
	// NaturalRelease is true when readRun reserved natural finalization before
	// Close claimed the client, so no group kill was sent.
	NaturalRelease bool
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

// DecideClientCloseRelease answers whether Close may count a run client as
// released. When readRun reserved natural finalization without a group kill,
// descendants without open pipes are not proven stopped: once the natural
// release ends it is not confirmed (while running it is pending). A killed
// client requires the kill to have finished and confirmed termination,
// tree-kill support, and DecideRelease confirmation of Wait, reap, stdin, and
// pipe closure.
func DecideClientCloseRelease(f ClientCloseReleaseFacts) ReleaseDecision {
	if f.NaturalRelease {
		if !f.Release.Finished {
			return ReleaseDecision{Kind: ReleasePending, Reason: "the owned OpenCode run client's natural finalization is still running"}
		}
		if f.TreeKillSupported {
			return ReleaseDecision{Kind: ReleaseFailed, Reason: "the owned OpenCode run client finalized naturally without terminating its process group; descendants are not proven stopped"}
		}
		return ReleaseDecision{Kind: ReleaseFailed, Reason: "only the owned OpenCode run client root could be terminated; a descendant may still run"}
	}
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
