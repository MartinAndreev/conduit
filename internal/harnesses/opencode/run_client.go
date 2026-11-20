package opencode

import (
	"context"
	"errors"
	"sync"

	"github.com/MartinAndreev/conduit/internal/harnesses/opencode/rules"
	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// runClient is the single owner of one `opencode run` child's termination and
// release. readRun, Close, watchServer, killRunClient and stdin-close cleanup
// all coordinate through it:
//   - finalization ownership is reserved atomically under the session mutex
//     (s.mu) via claimLocked and rules.DecideClientClaim;
//   - at most one KillTree is ever sent, followed immediately by
//     RequestFinalize, and every caller shares its outcome;
//   - at most one Wait and one post-Wait Finalize observer run for the whole
//     lifetime of the client, so repeated Close calls only await that shared
//     observer without spawning more goroutines.
type runClient struct {
	proc processruntime.Process

	// owner is read and updated only while holding the owning session's mu.
	owner rules.ClientOwner

	killOnce sync.Once
	killDone chan struct{}
	killErr  error

	waitOnce   sync.Once
	waitDone   chan struct{}
	waitResult processruntime.Result

	releaseOnce sync.Once
	releaseDone chan struct{}
	finalizeErr error
	releaseErr  error
}

func newRunClient(proc processruntime.Process) *runClient {
	return &runClient{
		proc:        proc,
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
	return decision
}

// terminate sends the client's single group kill, registers finalization
// immediately after it (independently of Wait and pipes), and returns that
// kill's outcome to every caller.
func (c *runClient) terminate() error {
	c.killOnce.Do(func() {
		c.killErr = c.proc.KillTree()
		processruntime.RequestFinalize(c.proc)
		close(c.killDone)
	})
	<-c.killDone
	return c.killErr
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

// startRelease starts the client's single Wait + Finalize observer. When
// needsKill is true it waits for the single kill (and its RequestFinalize)
// before awaiting Finalize, so finalization never precedes the kill.
func (c *runClient) startRelease(needsKill bool) <-chan struct{} {
	c.releaseOnce.Do(func() {
		waitDone := c.startWait()
		go func() {
			if needsKill {
				<-c.killDone
			}
			<-waitDone
			if finalizer, ok := c.proc.(processruntime.Finalizer); ok {
				c.finalizeErr = finalizer.Finalize(context.Background())
			}
			c.releaseErr = errors.Join(c.waitResult.ReleaseErr, c.finalizeErr)
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
		return rules.ReleaseFacts{
			Finished:      true,
			RootReaped:    c.finalizeErr == nil,
			ReleaseFailed: c.releaseErr != nil,
		}
	default:
		return rules.ReleaseFacts{}
	}
}

var _ processRelease = (*runClient)(nil)
