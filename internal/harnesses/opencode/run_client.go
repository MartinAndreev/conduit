package opencode

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	"github.com/MartinAndreev/conduit/internal/lockscope"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// runClient is the single owner of one `opencode run` child's termination and
// release. readRun, Close, watchServer, killRunClient and stdin-close cleanup
// all coordinate through it:
//   - finalization ownership is reserved atomically under the session mutex
//     (s.mu) via claimLocked and rules.DecideClientClaim;
//   - at most one kill worker sends KillTree followed immediately by
//     RequestFinalize, and callers wait for it within their own deadline;
//   - stdin is pending from publication until closeStdin records its outcome,
//     and forced stdout/stderr close errors are retained in the shared result;
//   - at most one Wait and one release observer run for the whole lifetime of
//     the client, so repeated Close calls only await that shared observer
//     without spawning more goroutines.
type runClient struct {
	proc processruntime.Process

	// owner is read and updated only while holding the owning session's mu.
	owner rules.ClientOwner
	// killedMode records whether claimLocked ever moved the client to a kill
	// path (ClientOwnerKilled or ClientOwnerClose), so decideRelease enforces
	// kill confirmation when the client is tracked in s.releases.
	killedMode atomic.Bool

	// stdinDone stays open (pending) from newRunClient until closeStdin
	// records Stdin().Close()'s outcome.
	stdinOnce sync.Once
	stdinDone chan struct{}
	stdinErr  error

	// pipeMu protects pipeCloseErr recorded when forced cleanup closes
	// stdout/stderr after a stdin-close failure.
	pipeMu       sync.Mutex
	pipesOnce    sync.Once
	pipeCloseErr error

	killOnce sync.Once
	killDone chan struct{}
	killErr  error

	waitOnce   sync.Once
	waitDone   chan struct{}
	waitResult processruntime.Result

	releaseOnce sync.Once
	releaseDone chan struct{}
	finalizeErr error
}

func newRunClient(proc processruntime.Process) *runClient {
	return &runClient{
		proc:        proc,
		stdinDone:   make(chan struct{}),
		killDone:    make(chan struct{}),
		waitDone:    make(chan struct{}),
		releaseDone: make(chan struct{}),
	}
}

// claimLocked evaluates and applies a finalization/termination reservation.
// The owning session's mu must be held.
func (c *runClient) claimLocked(actor rules.ClientClaimActor) rules.ClientClaimDecision {
	decision := rules.DecideClientClaim(rules.ClientClaimFacts{Current: c.owner, Actor: actor})
	c.owner = decision.NextOwner
	if decision.NextOwner == rules.ClientOwnerKilled || decision.NextOwner == rules.ClientOwnerClose {
		c.killedMode.Store(true)
	}
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

// closePipes forces stdout and stderr closed once (used after a stdin-close
// failure when cleanup is refused or times out) and records any close errors
// in the shared release outcome.
func (c *runClient) closePipes() error {
	c.pipesOnce.Do(func() {
		err := errors.Join(closeCapturePipe(c.proc.Stdout()), closeCapturePipe(c.proc.Stderr()))
		_ = lockscope.WithMutexLock(&c.pipeMu, func() error {
			c.pipeCloseErr = err
			return nil
		})
	})
	return c.pipeCloseError()
}

func (c *runClient) pipeCloseError() error {
	var err error
	_ = lockscope.WithMutexLock(&c.pipeMu, func() error {
		err = c.pipeCloseErr
		return nil
	})
	return err
}

// startTerminate launches the client's single kill worker (at most one per
// client). The worker sends KillTree, registers finalization immediately
// after it (independently of Wait and pipes), stores the outcome, and closes
// killDone. Callers never block inside sync.Once.Do.
func (c *runClient) startTerminate() <-chan struct{} {
	c.killOnce.Do(func() {
		go func() {
			c.killErr = c.proc.KillTree()
			processruntime.RequestFinalize(c.proc)
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
// that closes once Wait completed.
func (c *runClient) startWait() <-chan struct{} {
	c.waitOnce.Do(func() {
		go func() {
			c.waitResult = c.proc.Wait()
			close(c.waitDone)
		}()
	})
	return c.waitDone
}

// wait blocks until the shared Wait observer completes and returns its result.
func (c *runClient) wait() processruntime.Result {
	<-c.startWait()
	return c.waitResult
}

// observeRootExit reports root exit independently of output pipes when proc
// implements RootExitObserver, and otherwise shares the single Wait observer.
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
// recorded stdin's outcome), for the single kill worker when needsKill is
// true (so finalization never precedes the kill), for the single Wait, and
// then for Finalize.
func (c *runClient) startRelease(needsKill bool) <-chan struct{} {
	c.releaseOnce.Do(func() {
		waitDone := c.startWait()
		go func() {
			<-c.stdinDone
			if needsKill {
				<-c.killDone
			}
			<-waitDone
			if finalizer, ok := c.proc.(processruntime.Finalizer); ok {
				c.finalizeErr = finalizer.Finalize(context.Background())
			}
			close(c.releaseDone)
		}()
	})
	return c.releaseDone
}

// awaitRelease ensures the shared release observer is running, waits for it
// bounded by ctx, and returns the current release facts.
func (c *runClient) awaitRelease(ctx context.Context, needsKill bool) rules.ReleaseFacts {
	done := c.startRelease(needsKill)
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

func (c *runClient) closeReleaseFacts(natural bool) rules.ClientCloseReleaseFacts {
	killFinished, killErr := c.killOutcome()
	return rules.ClientCloseReleaseFacts{
		NaturalRelease:    natural,
		KillFinished:      killFinished,
		KillConfirmed:     killFinished && processruntime.KillConfirmed(killErr),
		TreeKillSupported: c.proc.TreeKillSupported(),
		Release:           c.releaseFacts(),
	}
}

func (c *runClient) decideRelease() rules.ReleaseDecision {
	if c.killedMode.Load() {
		return rules.DecideClientCloseRelease(c.closeReleaseFacts(false))
	}
	return rules.DecideRelease(c.releaseFacts())
}

var _ processRelease = (*runClient)(nil)
