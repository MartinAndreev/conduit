package process

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

// Spec describes an owned child process. Environment entries use KEY=VALUE form.
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
	// ReplaceEnv selects how Env is applied. When true, Env is the complete
	// child environment, even when empty, so inherited variables can be
	// removed. When false, a non-nil Env is appended to the inherited
	// os.Environ (the last duplicate key wins) and a nil Env inherits it.
	ReplaceEnv bool
}

// Result is the observed result of reaping an owned process.
type Result struct {
	ReleaseErr error
	ExitCode   int
	Signal     string
	Err        error
	ExitedAt   time.Time
}

// Launcher starts owned processes without binding their lifetime to a request
// context. Callers must explicitly interrupt or kill every started process.
type Launcher interface {
	Start(Spec) (Process, error)
}

// Process exposes the pipes and lifecycle of one owned process. Callers must
// continuously drain stdout and stderr. Wait is safe to call concurrently,
// waits for the root's exit and for those readers to reach EOF/close, and
// always returns the same result; it does not mean the root was reaped (see
// Finalize).
type Process interface {
	PID() int
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() Result
	Interrupt() error
	KillTree() error
	TreeKillSupported() bool
}

// ExecLauncher starts OS processes using the standard library.
type ExecLauncher struct{}

// RootExitObserver is implemented by a Process that reports root exit
// independently of its output pipes. ExecLauncher processes implement it.
type RootExitObserver interface {
	// RootExited closes once the root process exited, even while
	// descendants keep stdout or stderr open (Wait still blocks then). The
	// root may not be reaped yet: on Linux and Darwin, ExecLauncher keeps it
	// unreaped until Finalize, so KillTree still reaches its descendants
	// without signalling a reused process group.
	RootExited() <-chan struct{}
	// RootResult reports the root's exit status; it blocks until RootExited
	// closed. Owned-resource release is reported only by Wait.
	RootResult() Result
}

// ObserveRootExit returns a channel that closes when the root of p exited and
// a function returning its result once it did. When p does not implement
// RootExitObserver it falls back to Wait, which can report the exit only after
// both output pipes ended.
func ObserveRootExit(p Process) (<-chan struct{}, func() Result) {
	if observer, ok := p.(RootExitObserver); ok {
		return observer.RootExited(), observer.RootResult
	}
	exited := make(chan struct{})
	var result Result
	go func() {
		result = p.Wait()
		close(exited)
	}()
	return exited, func() Result {
		<-exited
		return result
	}
}

// Finalizer is implemented by a Process whose root stays unreaped, and so
// signallable, until its owner declares that it sends no more signals.
// ExecLauncher processes implement it.
// A decorator that embeds a Process must forward Finalize (and
// RootExitObserver), for example with a compile-time assertion such as
// var _ Finalizer = (*wrapper)(nil); otherwise Finalize falls back to Wait
// and the root is never released.
type Finalizer interface {
	// Finalize registers that the owner sends no more signals (it never
	// kills) and waits, bounded by ctx, for the release. The process owns
	// the release from the first call on: once the root exited, signals are
	// refused and the root is released, even after ctx ended. Before the
	// exit, signals still pass. It is idempotent and safe to call
	// concurrently. A failed release is returned and keeps signals refused.
	Finalize(ctx context.Context) error
}

// Finalize ends the owner's signalling of p: every owner path must end in it,
// after its last KillTree or Interrupt. When p does not implement Finalizer it
// waits for Wait, bounded by ctx.
func Finalize(ctx context.Context, p Process) error {
	if finalizer, ok := p.(Finalizer); ok {
		return finalizer.Finalize(ctx)
	}
	done := make(chan struct{})
	go func() {
		p.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RequestFinalize registers the end of the owner's signalling without
// waiting: call it right after the last signal, independently of Wait and
// pipe completion. A Finalizer then releases the root once it exited. It does
// nothing for a Process without Finalizer.
func RequestFinalize(p Process) {
	if finalizer, ok := p.(Finalizer); ok {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = finalizer.Finalize(ctx)
	}
}

// FinalizeWithin finalizes p, waiting at most bound. A Finalizer keeps
// finalizing after the bound (see Finalize), so ownership still ends once the
// root exits; the caller reports the release as unconfirmed. A nil result
// means p was released within the bound.
func FinalizeWithin(p Process, bound time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	return Finalize(ctx, p)
}

// KillConfirmed reports whether a KillTree result confirms termination of the
// group: delivered (nil), or the OS found it empty (os.ErrProcessDone). A
// refusal by finalization (ErrSignalsFinalized) sent nothing and confirms
// nothing; the owner must rely on the outcome of its earlier kill.
func KillConfirmed(err error) bool {
	return err == nil || (errors.Is(err, os.ErrProcessDone) && !errors.Is(err, ErrSignalsFinalized))
}
