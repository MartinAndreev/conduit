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
