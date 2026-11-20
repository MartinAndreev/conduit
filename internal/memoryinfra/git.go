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
	// finished, or for termination and reap after cancellation. It starts
	// when git finished or the caller cancelled, never before. On Darwin
	// process confirms the reap by polling with a backoff whose checks land
	// near 0, 10, 30, 70, 150 and 310 ms, so a budget near 250 ms would
	// report a false cleanup failure; one second leaves margin.
	gitCleanupBudget = time.Second
	// hashLineBytes is the largest output line per path: a SHA-256 hash and
	// CRLF. Output beyond lines*hashLineBytes is not read into memory.
	hashLineBytes = 64 + 2
)

// run starts git with args in dir, writes stdinLines one per line and closes
// stdin, and collects at most expectedLines*hashLineBytes of stdout; stderr is
// drained and discarded. ok reports exit status 0. Exit 0 with stdout over
// its bound is ErrGitOutput. The caller's wait is bounded by ctx and
// the cleanup budget (gitCleanupBudget), which starts only after git
// finished or ctx ended. When git finished on its own (root exited, pipes ended)
// it is never signalled, only released (releaseFinished). On cancellation a
// process.CleanupOwner kills the process tree, which reaches the group also
// after the root exited (process keeps the exited root unreaped until
// finalization), closes the parent's pipe ends, and finalizes the root.
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
	// The budget starts only now: it measures cleanup, not git's own run.
	cleanupCtx, stop := context.WithTimeout(context.Background(), w.cleanupBudget)
	defer stop()
	if finished {
		return releaseFinished(cleanupCtx, child, stdout)
	}
	// Cancelled. The owner is created only now: it closes stdin, which the
	// writer owned until here. Its kill is the last signal; it registers
	// finalization right after it.
	owner := process.NewCleanupOwnerWithDrain(child, piped)
	owner.BeginCleanup()
	state, waitErr := owner.Await(cleanupCtx)
	if cause := errors.Join(waitErr, state.TreeTerminationErr, state.PipeCloseErr, state.StdinCloseErr, state.RootResult.ReleaseErr); cause != nil {
		return nil, false, errors.Join(ctx.Err(), &GitCleanup{done: owner.Done(), state: owner.Latest, cause: cause})
	}
	return nil, false, ctx.Err()
}

// releaseFinished collects the result of a git process whose root exited and
// whose pipes ended, without signalling it: Wait releases the owned
// resources, then finalization releases the root. An unconfirmed or failed
// release fails the call with a GitCleanup observer.
func releaseFinished(ctx context.Context, child process.Process, stdout *boundedBuffer) ([]byte, bool, error) {
	release := startFinishedRelease(child)
	select {
	case <-release.done:
	case <-ctx.Done():
		return nil, false, &GitCleanup{done: release.done, state: release.state, cause: ctx.Err()}
	}
	state := release.state()
	if err := state.RootResult.ReleaseErr; err != nil {
		return nil, false, &GitCleanup{done: release.done, state: release.state, cause: err}
	}
	ok := state.RootResult.Err == nil && state.RootResult.ExitCode == 0
	data, exceeded := stdout.result()
	if ok && exceeded {
		return nil, false, ErrGitOutput
	}
	return data, ok, nil
}

// finishedRelease publishes the non-signalling release of a finished git
// process: Wait's result, then finalization's terminal outcome joined into
// RootResult.ReleaseErr. RootReaped and Complete are set before done closes.
type finishedRelease struct {
	mu     sync.Mutex
	latest process.CleanupState
	done   chan struct{}
}

func startFinishedRelease(child process.Process) *finishedRelease {
	release := &finishedRelease{done: make(chan struct{})}
	go func() {
		result := child.Wait()
		// git was never signalled: finalization only ends ownership and
		// releases the root.
		finalizeErr := process.Finalize(context.Background(), child)
		_ = lockscope.WithMutexLock(&release.mu, func() error {
			release.latest.RootResult = result
			release.latest.RootResult.ReleaseErr = errors.Join(result.ReleaseErr, finalizeErr)
			release.latest.RootReaped = finalizeErr == nil
			release.latest.ReadersDone = true
			release.latest.Complete = true
			return nil
		})
		close(release.done)
	}()
	return release
}

func (r *finishedRelease) state() (state process.CleanupState) {
	_ = lockscope.WithMutexLock(&r.mu, func() error {
		state = r.latest
		return nil
	})
	return state
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

func (e *GitCleanup) Error() string   { return ErrGitCleanup.Error() }
func (e *GitCleanup) Unwrap() []error { return []error{ErrGitCleanup, e.cause} }

// Done closes when the outstanding work finished: the release of a finished
// process, or the owner's single cleanup attempt after cancellation.
func (e *GitCleanup) Done() <-chan struct{} { return e.done }

// State is the latest state of the outstanding work. After cancellation it
// is the owner's (tree kill, pipe closes, drain, finalization). For a process
// that finished on its own it is the release: RootResult (with the release
// error) and RootReaped once finalization ended; no termination is requested.
// Complete is set before Done closes.
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
