package process

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/MartinAndreev/conduit/internal/lockscope"
)

// CleanupState separates reaping the root from the result of requesting tree
// termination. A successful signal is not proof that the OS is quiescent.
// ClosureState distinguishes a pending Close from completion with failure.
type ClosureState struct {
	Complete bool
	Err      error
}
type CleanupState struct {
	StdinClosure  ClosureState
	StdoutClosure ClosureState
	StderrClosure ClosureState

	TreeTerminationRequested bool
	TreeTerminationErr       error
	PipesClosed              bool
	PipeCloseErr             error
	StdinCloseErr            error
	ReadersDone              bool
	RootReaped               bool
	RootResult               Result
	Complete                 bool
}

// CleanupOwner retains responsibility for one process after a caller stops
// waiting. It performs one reap and one cleanup attempt, without retrying.
type CleanupOwner struct {
	child       Process
	mu          sync.Mutex
	state       CleanupState
	begin       sync.Once
	rootDone    chan struct{}
	done        chan struct{}
	stdinDone   chan struct{}
	drainDone   <-chan struct{}
	readersDone chan struct{}
}

func NewCleanupOwner(child Process) *CleanupOwner {
	done := make(chan struct{})
	close(done)
	return NewCleanupOwnerWithDrain(child, done)
}

// NewCleanupOwnerWithDrain includes the caller's capture drain workers in
// completion. Closing stdin is asynchronous, so even a slow Close stays owned.
func NewCleanupOwnerWithDrain(child Process, drainDone <-chan struct{}) *CleanupOwner {
	o := &CleanupOwner{child: child, rootDone: make(chan struct{}), done: make(chan struct{}), stdinDone: make(chan struct{}), drainDone: drainDone, readersDone: make(chan struct{})}
	go func() {
		err := closeCapturePipe(child.Stdin())
		_ = lockscope.WithMutexLock(&o.mu, func() error {
			o.state.StdinCloseErr = err
			o.state.StdinClosure = ClosureState{Complete: true, Err: err}
			return nil
		})
		close(o.stdinDone)
	}()
	go func() {
		<-o.drainDone
		_ = lockscope.WithMutexLock(&o.mu, func() error { o.state.ReadersDone = true; return nil })
		close(o.readersDone)
	}()
	go func() {
		result := child.Wait()
		_ = lockscope.WithMutexLock(&o.mu, func() error {
			o.state.RootResult = result
			o.state.RootReaped = true
			return nil
		})
		close(o.rootDone)
	}()
	return o
}
func (o *CleanupOwner) RootDone() <-chan struct{} { return o.rootDone }
func (o *CleanupOwner) Done() <-chan struct{}     { return o.done }
func (o *CleanupOwner) Latest() (state CleanupState) {
	_ = lockscope.WithMutexLock(&o.mu, func() error { state = o.state; return nil })
	return
}
func (o *CleanupOwner) BeginCleanup() {
	o.begin.Do(func() {
		_ = lockscope.WithMutexLock(&o.mu, func() error { o.state.TreeTerminationRequested = true; return nil })
		go func() {
			killErr := o.child.KillTree()
			if errors.Is(killErr, os.ErrProcessDone) {
				killErr = nil
			}
			_ = lockscope.WithMutexLock(&o.mu, func() error { o.state.TreeTerminationErr = killErr; return nil })
			stdoutErr := closeCapturePipe(o.child.Stdout())
			_ = lockscope.WithMutexLock(&o.mu, func() error {
				o.state.StdoutClosure = ClosureState{Complete: true, Err: stdoutErr}
				o.state.PipeCloseErr = stdoutErr
				return nil
			})
			stderrErr := closeCapturePipe(o.child.Stderr())
			_ = lockscope.WithMutexLock(&o.mu, func() error {
				o.state.StderrClosure = ClosureState{Complete: true, Err: stderrErr}
				o.state.PipeCloseErr = errors.Join(stdoutErr, stderrErr)
				o.state.PipesClosed = o.state.PipeCloseErr == nil
				return nil
			})
			<-o.rootDone
			<-o.stdinDone
			<-o.readersDone
			_ = lockscope.WithMutexLock(&o.mu, func() error {
				o.state.Complete = true
				return nil
			})
			close(o.done)
		}()
	})
}
func (o *CleanupOwner) Await(ctx context.Context) (CleanupState, error) {
	select {
	case <-o.done:
		return o.Latest(), nil
	default:
	}
	select {
	case <-o.done:
		return o.Latest(), nil
	case <-ctx.Done():
		return o.Latest(), ctx.Err()
	}
}

// os.ErrClosed confirms a pipe was already released by its drain worker.
func closeCapturePipe(pipe io.Closer) error {
	err := pipe.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
