package process

import (
	"io"
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
// waits for those readers to reach EOF/close, and always returns the same result.
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
	// RootExited closes once the root process exited and was reaped, even
	// while descendants keep stdout or stderr open (Wait still blocks then).
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
