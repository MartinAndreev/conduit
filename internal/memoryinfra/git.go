package memoryinfra

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	"github.com/MartinAndreev/conduit/internal/process"
)

// ErrGitUnavailable: git could not be found on PATH or started. Its text
// never repeats the cause, which can name the workspace.
var ErrGitUnavailable = errors.New("git could not be started")

// ErrGitCleanup: the git process could not be confirmed reaped (and, after
// cancellation, terminated) within gitCleanupBudget. The error carries the cleanup owner
// (GitCleanup) so its eventual completion stays observable.
var ErrGitCleanup = errors.New("git process cleanup was not confirmed")

const (
	// gitTimeout bounds one ObserveFiles call's git work, the batch, the
	// control run and the per-file retries together, even when the caller's
	// context has no deadline: hashing files up to
	// memory.MaxSnapshotFileBytes, filters included, should take far less.
	gitTimeout = 2 * time.Minute
	// gitCleanupBudget bounds the caller's wait for the reap after git
	// finished, or for termination and reap after cancellation.
	gitCleanupBudget = 250 * time.Millisecond
	// hashLineBytes is the largest output line per path: a SHA-256 hash and
	// CRLF. Output beyond lines*hashLineBytes is not read into memory.
	hashLineBytes = 64 + 2
)

// run starts git with args in dir, writes stdinLines one per line and closes
// stdin, and collects at most expectedLines*hashLineBytes of stdout; stderr is
// drained and discarded. ok reports exit status 0. Exit 0 with stdout over
// its bound is ErrGitOutput. The caller's wait is bounded by ctx and
// gitCleanupBudget. When git finished on its own (root exited, pipes ended)
// it is only reaped, never signalled: its process group may already be gone
// and its number reused. Only on cancellation, before the root is known to
// be reaped, is the process tree killed and reaped by a
// process.CleanupOwner; a root found exited after cancellation is not
// signalled (releaseExited).
func (w *WorkspaceSnapshots) run(ctx context.Context, dir string, args, stdinLines []string, expectedLines int) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, false, opaque(ErrGitUnavailable, err)
	}
	child, err := w.launcher.Start(process.Spec{
		Path:       gitPath,
		Args:       args,
		Dir:        dir,
		Env:        gitEnvironment(os.Environ(), runtime.GOOS == "windows"),
		ReplaceEnv: true,
	})
	if err != nil {
		return nil, false, opaque(ErrGitUnavailable, err)
	}
	stdout := &boundedBuffer{limit: expectedLines * hashLineBytes}
	piped := pipe(child, stdinLines, stdout)
	rootExited, _ := process.ObserveRootExit(child)
	finished := false
	select {
	case <-allClosed(piped, rootExited):
		finished = true
	case <-ctx.Done():
	}
	// The owner is created only now: it closes stdin, which the writer
	// owned until here.
	owner := process.NewCleanupOwnerWithDrain(child, piped)
	cleanupCtx, stop := context.WithTimeout(context.Background(), gitCleanupBudget)
	defer stop()
	if finished {
		return reapFinished(cleanupCtx, owner, stdout)
	}
	// Cancelled. A root that already exited is not signalled: it may be
	// reaped already, so its group number may be reused. This narrows, but
	// does not close, the window between process's independent reap and
	// KillTree.
	if closed(rootExited) {
		return nil, false, releaseExited(ctx, cleanupCtx, child, owner, piped)
	}
	owner.BeginCleanup()
	state, waitErr := owner.Await(cleanupCtx)
	if cause := errors.Join(waitErr, state.TreeTerminationErr, state.PipeCloseErr, state.StdinCloseErr, state.RootResult.ReleaseErr); cause != nil {
		return nil, false, errors.Join(ctx.Err(), &GitCleanup{done: owner.Done(), state: owner.Latest, cause: cause})
	}
	return nil, false, ctx.Err()
}

// releaseExited ends a cancelled run whose root already exited, without
// signalling. A descendant (a filter) may still hold git's stdout or stderr,
// which keeps the drain workers and Wait blocked, so the parent's ends are
// closed. The closes run in their own goroutine: on Unix they unblock the
// pending reads at once, while on Windows a close may wait for a read in
// flight, which only the descendant's exit ends. The caller waits for the
// closes, the drain and the reap within the cleanup budget; otherwise the
// GitCleanup observer tracks all three, with each close result. The result
// is always the cancellation.
func releaseExited(ctx, cleanupCtx context.Context, child process.Process, owner *process.CleanupOwner, piped <-chan struct{}) error {
	release := &exitedRelease{owner: owner, done: make(chan struct{})}
	closing := make(chan struct{})
	go func() {
		release.closed(&release.stdout, closeOutput(child.Stdout()))
		release.closed(&release.stderr, closeOutput(child.Stderr()))
		close(closing)
	}()
	go func() {
		<-allClosed(closing, allClosed(piped, owner.RootDone()))
		release.complete()
	}()
	select {
	case <-release.done:
	case <-cleanupCtx.Done():
		return errors.Join(ctx.Err(), &GitCleanup{done: release.done, state: release.state, cause: cleanupCtx.Err()})
	}
	state := release.state()
	if cause := errors.Join(state.PipeCloseErr, state.RootResult.ReleaseErr); cause != nil {
		return errors.Join(ctx.Err(), &GitCleanup{done: release.done, state: release.state, cause: cause})
	}
	return ctx.Err()
}

// exitedRelease publishes the non-signalling cleanup of releaseExited: each
// pipe close result and completion (closes, drain and reap all done). Its
// state is the owner's (drain, reap, stdin) with these overlaid; complete is
// set before done closes.
type exitedRelease struct {
	owner          *process.CleanupOwner
	mu             sync.Mutex
	stdout, stderr process.ClosureState
	completed      bool
	done           chan struct{}
}

func (r *exitedRelease) closed(closure *process.ClosureState, err error) {
	_ = lockscope.WithMutexLock(&r.mu, func() error {
		*closure = process.ClosureState{Complete: true, Err: err}
		return nil
	})
}

func (r *exitedRelease) complete() {
	_ = lockscope.WithMutexLock(&r.mu, func() error {
		r.completed = true
		return nil
	})
	close(r.done)
}

func (r *exitedRelease) state() process.CleanupState {
	state := r.owner.Latest()
	_ = lockscope.WithMutexLock(&r.mu, func() error {
		state.StdoutClosure = r.stdout
		state.StderrClosure = r.stderr
		state.PipeCloseErr = errors.Join(r.stdout.Err, r.stderr.Err)
		state.PipesClosed = r.stdout.Complete && r.stderr.Complete && state.PipeCloseErr == nil
		state.Complete = r.completed
		return nil
	})
	return state
}

// closeOutput closes the parent's end of an output pipe; one already closed
// by its drain worker is not an error.
func closeOutput(pipe io.Closer) error {
	if err := pipe.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}

// reapFinished collects the result of a git process whose root exited and
// whose pipes ended, without signalling it.
func reapFinished(ctx context.Context, owner *process.CleanupOwner, stdout *boundedBuffer) ([]byte, bool, error) {
	select {
	case <-owner.RootDone():
	case <-ctx.Done():
		return nil, false, &GitCleanup{done: owner.RootDone(), state: reapState(owner), cause: ctx.Err()}
	}
	state := owner.Latest()
	if err := state.RootResult.ReleaseErr; err != nil {
		return nil, false, &GitCleanup{done: owner.RootDone(), state: reapState(owner), cause: err}
	}
	ok := state.RootResult.Err == nil && state.RootResult.ExitCode == 0
	data, exceeded := stdout.result()
	if ok && exceeded {
		return nil, false, ErrGitOutput
	}
	return data, ok, nil
}

// pipe writes stdin and drains stdout and stderr; the returned channel closes
// once all three are done. A write error means git exited or was killed
// early, which its exit status reports.
func pipe(child process.Process, stdinLines []string, stdout io.Writer) <-chan struct{} {
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		var input strings.Builder
		for _, line := range stdinLines {
			input.WriteString(line)
			input.WriteByte('\n')
		}
		_, _ = io.WriteString(child.Stdin(), input.String())
		_ = child.Stdin().Close()
	}()
	for _, drain := range []struct {
		reader io.ReadCloser
		writer io.Writer
	}{{child.Stdout(), stdout}, {child.Stderr(), io.Discard}} {
		go func() {
			defer workers.Done()
			_, _ = io.Copy(drain.writer, drain.reader)
			_ = drain.reader.Close()
		}()
	}
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	return done
}

// closed reports, without blocking, whether channel is closed.
func closed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

// allClosed closes once both channels closed.
func allClosed(first, second <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		<-first
		<-second
		close(done)
	}()
	return done
}

// GitCleanup reports a git process whose reap (and, after cancellation,
// termination) was not confirmed within the caller's budget. Its owner keeps
// going in the background.
type GitCleanup struct {
	done  <-chan struct{}
	state func() process.CleanupState
	cause error
}

// reapState is the state of a finished process's reap: the owner's, complete
// once the root was reaped (nothing else is outstanding).
func reapState(owner *process.CleanupOwner) func() process.CleanupState {
	return func() process.CleanupState {
		state := owner.Latest()
		state.Complete = state.RootReaped
		return state
	}
}

func (e *GitCleanup) Error() string   { return ErrGitCleanup.Error() }
func (e *GitCleanup) Unwrap() []error { return []error{ErrGitCleanup, e.cause} }

// Done closes when the outstanding work finished: the reap of a finished
// process, or the owner's single cleanup attempt after cancellation.
func (e *GitCleanup) Done() <-chan struct{} { return e.done }

// State is the latest state of the outstanding work. After cancellation of a
// running process it is the owner's (tree kill, pipe closes, reap). For a
// root found exited it also carries each pipe close result (StdoutClosure,
// StderrClosure, PipeCloseErr, PipesClosed). Complete is set before Done
// closes.
func (e *GitCleanup) State() process.CleanupState { return e.state() }

// boundedBuffer keeps at most limit bytes and records whether more arrived;
// it always accepts the write so the pipe keeps draining.
type boundedBuffer struct {
	mu       sync.Mutex
	limit    int
	data     []byte
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		available := b.limit - len(b.data)
		if len(p) > available {
			b.exceeded = true
			p = p[:available]
		}
		b.data = append(b.data, p...)
		return nil
	})
	return n, nil
}

func (b *boundedBuffer) result() (data []byte, exceeded bool) {
	_ = lockscope.WithMutexLock(&b.mu, func() error {
		data = slices.Clone(b.data)
		exceeded = b.exceeded
		return nil
	})
	return data, exceeded
}

// keptEnvironment are the inherited variables git needs: PATH to find its
// helpers and filters, HOME and XDG_CONFIG_HOME for the global configuration
// (filters such as LFS are configured there), TMPDIR for filters. On Windows
// the system variables a process needs are kept too, compared without case.
// Everything else, GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE, GIT_CONFIG_* and
// other overrides included, is not inherited.
var (
	keptEnvironment        = []string{"PATH", "HOME", "XDG_CONFIG_HOME", "TMPDIR"}
	keptWindowsEnvironment = []string{
		"SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP",
		"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA",
	}
	// setEnvironment disables prompts and optional lock taking.
	setEnvironment = []string{"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0"}
)

// gitEnvironment returns the complete child environment: the kept parent
// variables in parent order, then setEnvironment. windows selects the
// Windows variables and case-insensitive names.
func gitEnvironment(parent []string, windows bool) []string {
	kept := keptEnvironment
	if windows {
		kept = append(slices.Clone(keptEnvironment), keptWindowsEnvironment...)
	}
	var result []string
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if !found || name == "" {
			continue
		}
		if windows {
			name = strings.ToUpper(name)
		}
		if slices.Contains(kept, name) {
			result = append(result, entry)
		}
	}
	return append(result, setEnvironment...)
}
