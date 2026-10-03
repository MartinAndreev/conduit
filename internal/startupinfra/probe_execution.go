package startupinfra

import (
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/process"
	"io"
	"sync"
	"time"
)

// Cleanup has its own 250ms caller wait budget, separate from metadata's 3s.
// The owner and pipe readers continue after timeout until cleanup/reap finish.
const probeCleanupBudget = 250 * time.Millisecond

type probeExecution struct {
	result     process.Result
	timedOut   bool
	output     string
	exceeded   bool
	cleanupErr error
}

func (d Detector) executeProbe(ctx context.Context, path string, args []string) (probeExecution, error) {
	timeout := d.Timeout
	if timeout <= 0 || timeout > probeTimeout {
		timeout = probeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	launcher := d.Launcher
	if launcher == nil {
		launcher = process.ExecLauncher{}
	}
	if err := probeCtx.Err(); err != nil {
		return probeExecution{}, err
	}
	child, err := launcher.Start(process.Spec{Path: path, Args: args})
	if err != nil {
		if ctx.Err() != nil {
			return probeExecution{}, errors.Join(ctx.Err(), err)
		}
		return probeExecution{result: process.Result{ExitCode: -1, Err: err}}, nil
	}
	captured := &capture{}
	var readers sync.WaitGroup
	readers.Add(2)
	drained := make(chan struct{})
	for _, reader := range []io.ReadCloser{child.Stdout(), child.Stderr()} {
		go func(r io.ReadCloser) { defer readers.Done(); _, _ = io.Copy(captured, r); _ = r.Close() }(reader)
	}
	go func() { readers.Wait(); close(drained) }()
	owner := process.NewCleanupOwnerWithDrain(child, drained)
	execution := probeExecution{}
	select {
	case <-owner.RootDone():
	case <-probeCtx.Done():
		execution.timedOut = true
	}
	owner.BeginCleanup()
	cleanupCtx, stop := context.WithTimeout(context.Background(), probeCleanupBudget)
	defer stop()
	state, waitErr := owner.Await(cleanupCtx)
	execution.result = state.RootResult
	execution.output, execution.exceeded = captured.result()
	cause := errors.Join(waitErr, state.TreeTerminationErr, state.PipeCloseErr, state.StdinCloseErr, state.RootResult.ReleaseErr)
	if cause != nil {
		execution.cleanupErr = &probeCleanupFailure{cause: cause, observer: probeCleanupObserver{owner}}
	}
	return execution, nil
}
