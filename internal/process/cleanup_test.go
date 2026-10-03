package process

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type heldProcess struct {
	release chan struct{}
	cause   error
	kills   atomic.Int32
	waits   atomic.Int32
}

func (p *heldProcess) PID() int                { return 1 }
func (p *heldProcess) Stdin() io.WriteCloser   { return discardCloser{} }
func (p *heldProcess) Stdout() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (p *heldProcess) Stderr() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (p *heldProcess) Wait() Result            { p.waits.Add(1); <-p.release; return Result{ExitCode: 0} }
func (p *heldProcess) KillTree() error         { p.kills.Add(1); return p.cause }
func (p *heldProcess) Interrupt() error        { return nil }
func (p *heldProcess) TreeKillSupported() bool { return true }

type discardCloser struct{}

func (discardCloser) Write(b []byte) (int, error) { return len(b), nil }
func (discardCloser) Close() error                { return nil }
func TestCleanupOwnerRetainsReapAfterCallerTimeout(t *testing.T) {
	cause := errors.New("OS refused tree termination")
	child := &heldProcess{release: make(chan struct{}), cause: cause}
	owner := NewCleanupOwner(child)
	var callers sync.WaitGroup
	for range 16 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			owner.BeginCleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			state, err := owner.Await(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || state.RootReaped {
				t.Error(state, err)
			}
		}()
	}
	callers.Wait()
	if child.kills.Load() != 1 || child.waits.Load() != 1 || !errors.Is(owner.Latest().TreeTerminationErr, cause) {
		t.Fatal(owner.Latest())
	}
	close(child.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err := owner.Await(ctx)
	if err != nil || !state.Complete || !state.RootReaped || !errors.Is(state.TreeTerminationErr, cause) {
		t.Fatal(state, err)
	}
}

type slowClosePipe struct {
	release chan struct{}
	cause   error
}

func (p *slowClosePipe) Read([]byte) (int, error) { return 0, io.EOF }
func (p *slowClosePipe) Close() error             { <-p.release; return p.cause }

type slowCloseProcess struct {
	*heldProcess
	pipe *slowClosePipe
}

func (p slowCloseProcess) Stdout() io.ReadCloser { return p.pipe }
func TestSlowFailingPipeCloseRemainsObservable(t *testing.T) {
	killCause := errors.New("kill denied")
	closeCause := errors.New("pipe close failed")
	child := &heldProcess{release: make(chan struct{}), cause: killCause}
	pipe := &slowClosePipe{release: make(chan struct{}), cause: closeCause}
	owner := NewCleanupOwner(slowCloseProcess{child, pipe})
	owner.BeginCleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	state, err := owner.Await(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(state.TreeTerminationErr, killCause) || state.PipesClosed || state.StdoutClosure.Complete || !state.ReadersDone {
		t.Fatal(state, err)
	}
	close(pipe.release)
	close(child.release)
	doneCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	state, err = owner.Await(doneCtx)
	if err != nil || !state.Complete || state.PipesClosed || !errors.Is(state.PipeCloseErr, closeCause) || !state.StdoutClosure.Complete || !errors.Is(state.StdoutClosure.Err, closeCause) || !state.StderrClosure.Complete || !state.ReadersDone {
		t.Fatal(state, err)
	}
}

func TestReaderCompletionIsObservedSeparatelyFromPipeClosure(t *testing.T) {
	child := &heldProcess{release: make(chan struct{})}
	close(child.release)
	drain := make(chan struct{})
	owner := NewCleanupOwnerWithDrain(child, drain)
	owner.BeginCleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	state, err := owner.Await(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !state.RootReaped || !state.PipesClosed || !state.StdinClosure.Complete || state.ReadersDone || state.Complete {
		close(drain)
		t.Fatal(state, err)
	}
	close(drain)
	doneCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	state, err = owner.Await(doneCtx)
	if err != nil || !state.Complete || !state.ReadersDone {
		t.Fatal(state, err)
	}
}
