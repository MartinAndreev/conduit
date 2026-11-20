package opencode

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

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
type ownerRelease struct {
	owner                *processruntime.CleanupOwner
	treeKillNotSupported bool
}

func (r ownerRelease) Done() <-chan struct{} { return r.owner.Done() }
func (r ownerRelease) decideRelease() rules.ReleaseDecision {
	state := r.owner.Latest()
	return rules.DecideCancelledControlRelease(rules.CancelledControlReleaseFacts{
		TreeKillFailed:    state.TreeTerminationErr != nil,
		TreeKillSupported: !r.treeKillNotSupported,
		Release:           cleanupReleaseFacts(state),
	})
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

// closeCapturePipe closes one child pipe end, treating os.ErrClosed as
// already released (matching internal/process/cleanup.go).
func closeCapturePipe(pipe io.Closer) error {
	err := pipe.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

// controlStdinSignal observes the single Stdin().Close() call performed by
// process.CleanupOwner via a channel instead of polling, without modifying
// internal/process.
type controlStdinSignal struct {
	inner io.WriteCloser
	once  sync.Once
	done  chan struct{}
	err   error
}

func newControlStdinSignal(inner io.WriteCloser) *controlStdinSignal {
	return &controlStdinSignal{inner: inner, done: make(chan struct{})}
}

func (s *controlStdinSignal) Write(p []byte) (int, error) { return s.inner.Write(p) }

func (s *controlStdinSignal) Close() error {
	err := s.inner.Close()
	s.once.Do(func() {
		if !errors.Is(err, os.ErrClosed) {
			s.err = err
		}
		close(s.done)
	})
	return err
}

func (s *controlStdinSignal) await(ctx context.Context) (completed bool, closeErr error, waitErr error) {
	if s == nil {
		return true, nil, nil
	}
	select {
	case <-s.done:
		return true, s.err, nil
	default:
	}
	select {
	case <-s.done:
		return true, s.err, nil
	case <-ctx.Done():
		return false, nil, ctx.Err()
	}
}

type controlStdinBase struct {
	processruntime.Process
	stdin io.WriteCloser
}

func (p controlStdinBase) Stdin() io.WriteCloser { return p.stdin }

type controlStdinFinalizer struct {
	controlStdinBase
	finalizer processruntime.Finalizer
}

func (p controlStdinFinalizer) Finalize(ctx context.Context) error {
	return p.finalizer.Finalize(ctx)
}

type controlStdinRootObserver struct {
	controlStdinBase
	observer processruntime.RootExitObserver
}

func (p controlStdinRootObserver) RootExited() <-chan struct{}       { return p.observer.RootExited() }
func (p controlStdinRootObserver) RootResult() processruntime.Result { return p.observer.RootResult() }

type controlStdinFull struct {
	controlStdinBase
	finalizer processruntime.Finalizer
	observer  processruntime.RootExitObserver
}

func (p controlStdinFull) Finalize(ctx context.Context) error { return p.finalizer.Finalize(ctx) }
func (p controlStdinFull) RootExited() <-chan struct{}        { return p.observer.RootExited() }
func (p controlStdinFull) RootResult() processruntime.Result  { return p.observer.RootResult() }

var (
	_ processruntime.Finalizer        = controlStdinFinalizer{}
	_ processruntime.RootExitObserver = controlStdinRootObserver{}
	_ processruntime.Finalizer        = controlStdinFull{}
	_ processruntime.RootExitObserver = controlStdinFull{}
)

// wrapControlStdin wraps proc's Stdin with a signalling closer while
// preserving the exact Finalizer and RootExitObserver interface set that proc
// implements (so CleanupOwner's type assertions see the same capabilities).
func wrapControlStdin(proc processruntime.Process) (processruntime.Process, *controlStdinSignal) {
	sig := newControlStdinSignal(proc.Stdin())
	base := controlStdinBase{Process: proc, stdin: sig}
	fin, hasFin := proc.(processruntime.Finalizer)
	obs, hasObs := proc.(processruntime.RootExitObserver)
	switch {
	case hasFin && hasObs:
		return controlStdinFull{controlStdinBase: base, finalizer: fin, observer: obs}, sig
	case hasFin:
		return controlStdinFinalizer{controlStdinBase: base, finalizer: fin}, sig
	case hasObs:
		return controlStdinRootObserver{controlStdinBase: base, observer: obs}, sig
	default:
		return base, sig
	}
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
// already returned result. The background observer has no deadline because it
// owns tracking Finalize and Stdin().Close() to their eventual OS outcome so
// Close can observe completion after the caller timed out.
func startFinishedRelease(proc processruntime.Process, result processruntime.Result, stdinSig *controlStdinSignal) *finishedRelease {
	release := &finishedRelease{done: make(chan struct{}), result: result}
	go func() {
		release.finalizeErr = processruntime.Finalize(context.Background(), proc)
		if stdinSig != nil {
			<-stdinSig.done
			release.stdinErr = stdinSig.err
		}
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
// completed without a confirmed group termination, reap, or pipe close. The
// session keeps release until it is confirmed.
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
		return ownerRelease{owner: pending.Owner, treeKillNotSupported: pending.treeKillNotSupported}, true
	}
	return nil, false
}
