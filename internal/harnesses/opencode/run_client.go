package opencode

import (
	"context"
	"errors"
	"sync"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// runClient is the single owner of one `opencode run` child's termination and
// release. readRun, Close, watchServer, killRunClient and stdin-close cleanup
// all coordinate through it:
//   - finalization and termination ownership is reserved atomically under the
//     session mutex (s.mu) via claimLocked and rules.DecideClientClaim;
//   - every exit (clean or failed) runs at most one kill worker that calls
//     KillTree and synchronously registers finalization before closing
//     killDone; callers wait within their own deadline;
//   - when proc implements RootExitObserver, Wait is invoked only after
//     killDone closes, so on Windows KillTree always runs before Wait closes
//     the Job Object;
//   - stdin is pending from publication until closeStdin records its outcome,
//     and stdout/stderr closure runs once in its own worker whose completion
//     always precedes releaseDone (capturing forced-close errors as well as
//     trackedReadCloser's idempotent Close errors discarded by Wait);
//   - at most one Wait and one release observer run for the whole lifetime of
//     the client, so repeated Close calls only await that shared observer
//     without spawning more goroutines.
type runClient struct {
	proc processruntime.Process

	// owner is read and updated only while holding the owning session's mu.
	owner rules.ClientOwner

	// stdinDone stays open (pending) from newRunClient until closeStdin
	// records Stdin().Close()'s outcome.
	stdinOnce sync.Once
	stdinDone chan struct{}
	stdinErr  error

	// Single stdout/stderr close worker. May be triggered early (forced
	// cleanup on stdin failure or post-root-exit timeout) or after Wait in
	// startRelease; its completion always precedes releaseDone.
	pipeMu       sync.Mutex
	pipesOnce    sync.Once
	pipesDone    chan struct{}
	pipeCloseErr error

	killOnce sync.Once
	killDone chan struct{}
	killErr  error

	finalizeOnce sync.Once
	finalizeDone chan struct{}
	finalizeErr  error

	waitOnce   sync.Once
	waitDone   chan struct{}
	waitResult processruntime.Result

	releaseOnce sync.Once
	releaseDone chan struct{}
}

func newRunClient(proc processruntime.Process) *runClient {
	return &runClient{
		proc:         proc,
		stdinDone:    make(chan struct{}),
		pipesDone:    make(chan struct{}),
		killDone:     make(chan struct{}),
		finalizeDone: make(chan struct{}),
		waitDone:     make(chan struct{}),
		releaseDone:  make(chan struct{}),
	}
}

// claimLocked evaluates and applies a termination/finalization reservation.
// The owning session's mu must be held.
func (c *runClient) claimLocked(actor rules.ClientClaimActor) rules.ClientClaimDecision {
	decision := rules.DecideClientClaim(rules.ClientClaimFacts{Current: c.owner, Actor: actor})
	c.owner = decision.NextOwner
	return decision
}

// closeStdin closes the client's stdin once, records its outcome for the
// shared release observer, and closes stdinDone.
func (c *runClient) closeStdin() error {
	c.stdinOnce.Do(func() {
		c.stdinErr = closeCapturePipe(c.proc.Stdin())
		close(c.stdinDone)
	})
	<-c.stdinDone
	return c.stdinErr
}

// startClosePipes launches the client's single stdout/stderr close worker.
// It is invoked either early (forced cleanup on stdin failure or post-root-exit
// timeout) or after Wait in startRelease (to observe trackedReadCloser's
// idempotent Close error that execProcess.Wait discards). The worker has no
// deadline because io.Closer.Close takes no context and must finish in the
// background so releaseDone reflects its terminal outcome while callers bound
// their own wait via awaitClosePipes.
func (c *runClient) startClosePipes() <-chan struct{} {
	c.pipesOnce.Do(func() {
		go func() {
			err := errors.Join(closeCapturePipe(c.proc.Stdout()), closeCapturePipe(c.proc.Stderr()))
			_ = lockscope.WithMutexLock(&c.pipeMu, func() error {
				c.pipeCloseErr = err
				return nil
			})
			close(c.pipesDone)
		}()
	})
	return c.pipesDone
}

// awaitClosePipes starts pipe closure and waits for it bounded by ctx. When
// ctx expires first, the worker keeps running and its completion still feeds
// releaseDone.
func (c *runClient) awaitClosePipes(ctx context.Context) error {
	done := c.startClosePipes()
	select {
	case <-done:
		return c.pipeCloseError()
	default:
	}
	select {
	case <-done:
		return c.pipeCloseError()
	case <-ctx.Done():
		return nil
	}
}

func (c *runClient) pipeCloseError() error {
	var err error
	_ = lockscope.WithMutexLock(&c.pipeMu, func() error {
		err = c.pipeCloseErr
		return nil
	})
	return err
}

// registerFinalize registers finalization synchronously right after KillTree
// (before killDone closes), matching process.RequestFinalize, and launches at
// most one background observer when finalization is still in progress
// (returned context.Canceled). When finalization already completed
// synchronously (nil or a terminal error) finalizeDone closes immediately
// without calling Finalize a second time. The background observer has no
// deadline because finalization owns reaping the root to completion after
// callers stop waiting.
func (c *runClient) registerFinalize() {
	c.finalizeOnce.Do(func() {
		finalizer, ok := c.proc.(processruntime.Finalizer)
		if !ok {
			close(c.finalizeDone)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := finalizer.Finalize(ctx)
		if !errors.Is(err, context.Canceled) {
			c.finalizeErr = err
			close(c.finalizeDone)
			return
		}
		go func() {
			c.finalizeErr = finalizer.Finalize(context.Background())
			close(c.finalizeDone)
		}()
	})
}

// startTerminate launches the client's single kill worker (at most one per
// client). The worker sends KillTree, synchronously registers finalization
// right after it (independently of Wait and pipes) before closing killDone,
// and stores the outcome. Callers never block inside sync.Once.Do; the worker
// itself has no deadline because KillTree takes no context and callers bound
// their wait via awaitTerminate.
func (c *runClient) startTerminate() <-chan struct{} {
	c.killOnce.Do(func() {
		go func() {
			c.killErr = c.proc.KillTree()
			c.registerFinalize()
			close(c.killDone)
		}()
	})
	return c.killDone
}

// awaitTerminate ensures the single kill worker is running and waits for it
// bounded by ctx. It reports whether the kill finished and its error (valid
// only when finished is true).
func (c *runClient) awaitTerminate(ctx context.Context) (finished bool, err error) {
	done := c.startTerminate()
	select {
	case <-done:
		return true, c.killErr
	default:
	}
	select {
	case <-done:
		return true, c.killErr
	case <-ctx.Done():
		return false, nil
	}
}

// killOutcome returns the kill worker's current status without blocking.
func (c *runClient) killOutcome() (finished bool, err error) {
	select {
	case <-c.killDone:
		return true, c.killErr
	default:
		return false, nil
	}
}

// startWait starts the client's single Wait observer and returns the channel
// that closes once Wait completed. When proc implements RootExitObserver,
// Wait runs only after killDone closed so on Windows KillTree always runs
// before Wait closes the Job Object. The worker has no deadline because
// Process.Wait takes no context and must be shared across callers.
func (c *runClient) startWait() <-chan struct{} {
	c.waitOnce.Do(func() {
		_, hasRootObserver := c.proc.(processruntime.RootExitObserver)
		go func() {
			if hasRootObserver {
				<-c.killDone
			}
			c.waitResult = c.proc.Wait()
			close(c.waitDone)
		}()
	})
	return c.waitDone
}

// observeRootExit reports root exit independently of output pipes when proc
// implements RootExitObserver (without invoking Wait ahead of KillTree), and
// otherwise shares the single Wait observer.
func (c *runClient) observeRootExit() (<-chan struct{}, func() processruntime.Result) {
	if observer, ok := c.proc.(processruntime.RootExitObserver); ok {
		return observer.RootExited(), observer.RootResult
	}
	done := c.startWait()
	return done, func() processruntime.Result {
		<-done
		return c.waitResult
	}
}

// startRelease starts the client's single release observer. It waits for
// stdin close (so Close cannot observe a premature release before StartTurn
// recorded stdin's outcome), for the single kill worker (starting forced pipe
// closure when stdin failed and kill failed), for the single Wait, for the
// single Finalize observer, and always for the single stdout/stderr close
// worker before closing releaseDone. The observer has no deadline because it
// aggregates the terminal outcomes of those OS-bound workers for bounded
// callers.
func (c *runClient) startRelease() <-chan struct{} {
	c.releaseOnce.Do(func() {
		waitDone := c.startWait()
		go func() {
			<-c.stdinDone
			<-c.killDone
			if c.stdinErr != nil && c.killErr != nil {
				c.startClosePipes()
			}
			<-waitDone
			<-c.finalizeDone
			<-c.startClosePipes()
			close(c.releaseDone)
		}()
	})
	return c.releaseDone
}

// awaitRelease ensures the shared release observer is running, waits for it
// bounded by ctx, and returns the current release facts.
func (c *runClient) awaitRelease(ctx context.Context) rules.ReleaseFacts {
	done := c.startRelease()
	select {
	case <-done:
		return c.releaseFacts()
	default:
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
	return c.releaseFacts()
}

func (c *runClient) Done() <-chan struct{} { return c.releaseDone }

func (c *runClient) releaseFacts() rules.ReleaseFacts {
	select {
	case <-c.releaseDone:
		return processReleaseFacts(true, c.waitResult.ReleaseErr, c.finalizeErr, errors.Join(c.stdinErr, c.pipeCloseError()))
	default:
		return processReleaseFacts(false, nil, nil, nil)
	}
}

func (c *runClient) closeReleaseFacts() rules.ClientCloseReleaseFacts {
	killFinished, killErr := c.killOutcome()
	return rules.ClientCloseReleaseFacts{
		KillFinished:      killFinished,
		KillConfirmed:     killFinished && processruntime.KillConfirmed(killErr),
		TreeKillSupported: c.proc.TreeKillSupported(),
		Release:           c.releaseFacts(),
	}
}

// decideRelease evaluates the same DecideClientCloseRelease rule whether the
// client is active in s.client or tracked in s.releases.
func (c *runClient) decideRelease() rules.ReleaseDecision {
	return rules.DecideClientCloseRelease(c.closeReleaseFacts())
}

var _ processRelease = (*runClient)(nil)
