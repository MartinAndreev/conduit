package startupinfra

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/process"
)

type probeCleanupFailure struct {
	cause    error
	observer probeCleanupObserver
}

func (e *probeCleanupFailure) Error() string                            { return "probe cleanup could not be confirmed" }
func (e *probeCleanupFailure) Unwrap() error                            { return errors.Join(startup.ErrProbeCleanup, e.cause) }
func (e *probeCleanupFailure) CleanupObserver() startup.CleanupObserver { return e.observer }

type probeCleanupObserver struct{ owner *process.CleanupOwner }

func (o probeCleanupObserver) Latest() startup.CleanupObservation {
	return cleanupObservation(o.owner.Latest())
}
func (o probeCleanupObserver) Done() <-chan struct{} { return o.owner.Done() }
func (o probeCleanupObserver) Await(ctx context.Context) (startup.CleanupObservation, error) {
	state, err := o.owner.Await(ctx)
	return cleanupObservation(state), err
}
func cleanupObservation(state process.CleanupState) startup.CleanupObservation {
	return startup.CleanupObservation{
		TreeTerminationRequested: state.TreeTerminationRequested,
		TreeTerminationErr:       state.TreeTerminationErr,
		PipeCloseErr:             state.PipeCloseErr,
		StdinCloseErr:            state.StdinCloseErr,
		ResourceReleaseErr:       state.RootResult.ReleaseErr,
		RootReaped:               state.RootReaped,
		Complete:                 state.Complete,
		ReadersDone:              state.ReadersDone,
		PipesClosed:              state.PipesClosed,
		StdinClosure:             startup.CleanupClosure{Complete: state.StdinClosure.Complete, Err: state.StdinClosure.Err},
		StdoutClosure:            startup.CleanupClosure{Complete: state.StdoutClosure.Complete, Err: state.StdoutClosure.Err},
		StderrClosure:            startup.CleanupClosure{Complete: state.StderrClosure.Complete, Err: state.StderrClosure.Err},
		Err:                      errors.Join(state.TreeTerminationErr, state.PipeCloseErr, state.StdinCloseErr, state.RootResult.ReleaseErr),
	}
}
