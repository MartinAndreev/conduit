package opencode

import (
	"context"
	"errors"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// processRelease is the observable release of one owned client process (a
// run client or a control command) that the session stopped waiting for
// before it was confirmed. The session keeps it until rules.DecideRelease
// confirms it; until then Close does not report resources released.
type processRelease interface {
	// Done closes once the release work ended.
	Done() <-chan struct{}
	// releaseFacts are the current facts; final once Done closed.
	releaseFacts() rules.ReleaseFacts
}

// ownerRelease is the release of a cancelled control command: its cleanup
// owner's single attempt (kill, pipe close, drain, finalization).
type ownerRelease struct{ owner *processruntime.CleanupOwner }

func (r ownerRelease) Done() <-chan struct{} { return r.owner.Done() }
func (r ownerRelease) releaseFacts() rules.ReleaseFacts {
	return cleanupReleaseFacts(r.owner.Latest())
}

// cleanupReleaseFacts are the release facts of a cleanup owner's state.
func cleanupReleaseFacts(state processruntime.CleanupState) rules.ReleaseFacts {
	return rules.ReleaseFacts{
		Finished:        state.Complete,
		RootReaped:      state.RootReaped,
		ReleaseFailed:   state.RootResult.ReleaseErr != nil,
		PipeCloseFailed: state.PipeCloseErr != nil || state.StdinCloseErr != nil,
	}
}

// finishedRelease observes the non-signalling release of a client process:
// Wait's own release (Result.ReleaseErr, for example closing a Windows Job
// Object) and finalization's terminal outcome. Finalization keeps its worker
// after a caller's bound, so a later Finalize returns that outcome.
type finishedRelease struct {
	done chan struct{}
	// result, finalizeErr and err are read only after done closed. err joins
	// result.ReleaseErr and finalizeErr.
	result      processruntime.Result
	finalizeErr error
	err         error
}

// startFinishedRelease observes the release of a client whose Wait already
// returned result (a finished control command or run client).
func startFinishedRelease(proc processruntime.Process, result processruntime.Result) *finishedRelease {
	return startRelease(proc, func() processruntime.Result { return result })
}

// startClientRelease observes the release of a client that may still run:
// Wait, then finalization.
func startClientRelease(proc processruntime.Process) *finishedRelease {
	return startRelease(proc, proc.Wait)
}

func startRelease(proc processruntime.Process, wait func() processruntime.Result) *finishedRelease {
	release := &finishedRelease{done: make(chan struct{})}
	go func() {
		release.result = wait()
		release.finalizeErr = processruntime.Finalize(context.Background(), proc)
		release.err = errors.Join(release.result.ReleaseErr, release.finalizeErr)
		close(release.done)
	}()
	return release
}

func (r *finishedRelease) Done() <-chan struct{} { return r.done }
func (r *finishedRelease) releaseFacts() rules.ReleaseFacts {
	select {
	case <-r.done:
		return rules.ReleaseFacts{Finished: true, RootReaped: r.finalizeErr == nil, ReleaseFailed: r.err != nil}
	default:
		return rules.ReleaseFacts{}
	}
}

// releasePendingError reports a control command whose release was not
// confirmed: a finished command whose finalization failed or did not end
// within its bound, or a cancelled command whose cleanup completed without
// a confirmed reap or pipe close. The session keeps release until it is
// confirmed.
type releasePendingError struct {
	release processRelease
	cause   error
}

func (e *releasePendingError) Error() string { return e.cause.Error() }
func (e *releasePendingError) Unwrap() error { return e.cause }

// trackedRelease returns the release a control operation's error carries:
// an unconfirmed release, or the owner of a cleanup that was still pending.
func trackedRelease(err error) (processRelease, bool) {
	var unconfirmed *releasePendingError
	if errors.As(err, &unconfirmed) {
		return unconfirmed.release, true
	}
	var pending *CleanupPendingError
	if errors.As(err, &pending) && pending.Owner != nil {
		return ownerRelease{owner: pending.Owner}, true
	}
	return nil, false
}
