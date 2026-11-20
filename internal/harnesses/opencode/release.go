package opencode

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// processRelease is the observable release of one owned client process (a
// run client or a control command) that the session stopped waiting for
// before it was confirmed. The session keeps it until decideRelease confirms
// it; until then Close does not report resources released.
type processRelease interface {
	// Done closes once the release work ended.
	Done() <-chan struct{}
	// decideRelease evaluates the current release decision; final once Done
	// closed.
	decideRelease() rules.ReleaseDecision
}

// ownerRelease is the release of a cancelled control command: its cleanup
// owner's single attempt (kill, pipe close, drain, finalization).
type ownerRelease struct{ owner *processruntime.CleanupOwner }

func (r ownerRelease) Done() <-chan struct{} { return r.owner.Done() }
func (r ownerRelease) decideRelease() rules.ReleaseDecision {
	return rules.DecideRelease(cleanupReleaseFacts(r.owner.Latest()))
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

// processReleaseFacts builds rules.ReleaseFacts from the common release
// outcomes of Wait, Finalize, and pipe/stdin closure. Both finishedRelease
// (control commands) and runClient (run clients) use it without sharing
// each other's side effects.
func processReleaseFacts(finished bool, waitReleaseErr, finalizeErr, pipeCloseErr error) rules.ReleaseFacts {
	if !finished {
		return rules.ReleaseFacts{}
	}
	return rules.ReleaseFacts{
		Finished:        true,
		RootReaped:      finalizeErr == nil,
		ReleaseFailed:   waitReleaseErr != nil || finalizeErr != nil,
		PipeCloseFailed: pipeCloseErr != nil,
	}
}

// awaitOwnerStdin waits, bounded by ctx, for owner's asynchronous stdin close
// to complete. When owner is nil it reports an already completed close.
func awaitOwnerStdin(ctx context.Context, owner *processruntime.CleanupOwner) (processruntime.ClosureState, error) {
	if owner == nil {
		return processruntime.ClosureState{Complete: true}, nil
	}
	if closure := owner.Latest().StdinClosure; closure.Complete {
		return closure, nil
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			closure := owner.Latest().StdinClosure
			if closure.Complete {
				return closure, nil
			}
			return closure, ctx.Err()
		case <-ticker.C:
			if closure := owner.Latest().StdinClosure; closure.Complete {
				return closure, nil
			}
		}
	}
}

// closeCapturePipe closes one child pipe end, treating os.ErrClosed as
// already released (matching internal/process/cleanup.go).
func closeCapturePipe(pipe io.Closer) error {
	err := pipe.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

// finishedRelease observes the non-signalling release of a finished control
// command: Wait's own release (Result.ReleaseErr, for example closing a
// Windows Job Object), finalization's terminal outcome, and the cleanup
// owner's asynchronous stdin close.
type finishedRelease struct {
	done chan struct{}
	// result, finalizeErr and stdinErr are read only after done closed.
	result      processruntime.Result
	finalizeErr error
	stdinErr    error
}

// startFinishedRelease observes the release of a control command whose Wait
// already returned result.
func startFinishedRelease(proc processruntime.Process, result processruntime.Result, owner *processruntime.CleanupOwner) *finishedRelease {
	release := &finishedRelease{done: make(chan struct{}), result: result}
	go func() {
		release.finalizeErr = processruntime.Finalize(context.Background(), proc)
		stdinClosure, _ := awaitOwnerStdin(context.Background(), owner)
		release.stdinErr = stdinClosure.Err
		close(release.done)
	}()
	return release
}

func (r *finishedRelease) Done() <-chan struct{} { return r.done }

func (r *finishedRelease) releaseFacts() rules.ReleaseFacts {
	select {
	case <-r.done:
		return processReleaseFacts(true, r.result.ReleaseErr, r.finalizeErr, r.stdinErr)
	default:
		return processReleaseFacts(false, nil, nil, nil)
	}
}

func (r *finishedRelease) decideRelease() rules.ReleaseDecision {
	return rules.DecideRelease(r.releaseFacts())
}

// releasePendingError reports a control command whose release was not
// confirmed: a finished command whose finalization or stdin close failed or
// did not end within its bound, or a cancelled command whose cleanup
// completed without a confirmed reap or pipe close. The session keeps
// release until it is confirmed.
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
