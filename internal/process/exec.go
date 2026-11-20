package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MartinAndreev/conduit/internal/lockscope"
)

func (ExecLauncher) Start(spec Spec) (started Process, err error) {
	if spec.Path == "" {
		return nil, errors.New("process executable path is empty")
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	switch {
	case spec.ReplaceEnv:
		cmd.Env = append([]string{}, spec.Env...)
	case spec.Env != nil:
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	owned, err := prepareOwnedProcess(cmd)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(err, owned.close())
		}
	}()
	pipes, err := newStdioPipes()
	if err != nil {
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pipes.childStdin, pipes.childStdout, pipes.childStderr
	if err := cmd.Start(); err != nil {
		pipes.closeAll()
		return nil, err
	}
	// The child holds its own copies now; the parent keeps only its ends, so
	// stdout/stderr reach EOF once the root and every descendant closed them.
	pipes.closeChildEnds()
	if err := owned.activate(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		pipes.closeParentEnds()
		return nil, err
	}
	transferred = true
	proc := newExecProcess(cmd.Process.Pid, newRootOps(cmd, owned), owned, pipes.stdin, pipes.stdout, pipes.stderr)
	proc.counted = true
	unfinalized.Add(1)
	go proc.superviseRoot()
	return proc, nil
}

// stdioPipes are created by the launcher instead of exec.Cmd's *Pipe helpers.
// The child's ends are *os.File values, so exec.Cmd starts no copy goroutines
// and its Wait does not close the parent's read ends; Wait can therefore reap
// the root as soon as it exits, while descendants may still hold the pipes.
type stdioPipes struct {
	stdin                                *os.File
	stdout, stderr                       *os.File
	childStdin, childStdout, childStderr *os.File
}

func newStdioPipes() (*stdioPipes, error) {
	var pipes stdioPipes
	var err error
	if pipes.childStdin, pipes.stdin, err = os.Pipe(); err != nil {
		return nil, err
	}
	if pipes.stdout, pipes.childStdout, err = os.Pipe(); err != nil {
		pipes.closeAll()
		return nil, err
	}
	if pipes.stderr, pipes.childStderr, err = os.Pipe(); err != nil {
		pipes.closeAll()
		return nil, err
	}
	return &pipes, nil
}

func (p *stdioPipes) closeChildEnds() {
	for _, file := range []*os.File{p.childStdin, p.childStdout, p.childStderr} {
		if file != nil {
			_ = file.Close()
		}
	}
}

func (p *stdioPipes) closeParentEnds() {
	for _, file := range []*os.File{p.stdin, p.stdout, p.stderr} {
		if file != nil {
			_ = file.Close()
		}
	}
}

func (p *stdioPipes) closeAll() {
	p.closeChildEnds()
	p.closeParentEnds()
}

// execProcess separates root exit from root reap, so that a group signal
// never reaches a numeric PGID after the reap released it for reuse.
//
// A Unix root is addressed by number: KillTree and Interrupt signal its process
// group (PGID == root PID). While the root is an unreaped zombie its number
// cannot be reused, whatever happened to the rest of the group. On Linux and
// Darwin superviseRoot therefore observes the exit without reaping (see
// awaitRootExit) and publishes RootExited and RootResult; the root stays
// unreaped, and KillTree keeps reaching every group member, until the owner
// calls Finalize to declare that it sends no more signals. The first Finalize
// starts one finalization worker per process. Once the root exited, the
// worker commits under signalMu (reaped is set, so no group signal is in
// flight or sent afterwards: Interrupt and KillTree return ErrSignalsFinalized
// without a syscall), then reaps outside the lock once the root is
// authoritatively waitable without blocking, retrying a transient negative
// answer with backoff. When waitability can never be established, the
// outcome is ErrReleaseUnavailable: the root stays unreaped and counted.
// Every outcome is terminal and returned to every Finalize caller. Until the
// exit, the root is alive and signals still pass. Nothing reaps the root
// automatically, so every owner path must end in Finalize;
// UnfinalizedProcesses counts launched processes until their reap is
// established. Wait is independent of it: it returns when the pipes ended.
//
// Other Unix systems cannot observe the exit without reaping: the root is
// reaped at exit and group signals are refused from then on, so a descendant
// is no longer reachable through KillTree; Finalize only records the end of
// ownership. The kernel releases the PID inside exec.Cmd.Wait, just before
// signalMu records it, which leaves a short window in which a concurrent
// signal can still reach a reused number. Windows signals through handles
// (the kill-on-close job, the retained process handle), which cannot reach a
// reused PID, so its signals are not gated and Finalize only records the end
// of ownership.
type execProcess struct {
	pid    int
	ops    rootOps
	owned  *ownedResources
	stdin  io.WriteCloser
	stdout *trackedReadCloser
	stderr *trackedReadCloser
	// counted marks a process launched by ExecLauncher, which
	// UnfinalizedProcesses tracks until Finalize.
	counted bool

	// rootExited closes once the root exited; rootResult and held (the root
	// is still unreaped) are written before that close and read only after it.
	rootExited chan struct{}
	rootResult Result
	held       bool

	// signalMu orders group signals against the finalization commit, which
	// sets reaped (for a root reaped at exit, the exit sets it).
	signalMu sync.Mutex
	reaped   bool

	// finalizeOnce starts the single finalization worker; finalDone closes
	// once its terminal outcome is recorded, and finalErr is written before
	// that close and read only after it.
	finalizeOnce sync.Once
	finalDone    chan struct{}
	finalErr     error

	// released closes once both output pipes ended; waitResult is written
	// before that close and read only after it. supervised closes when
	// superviseRoot returned.
	released   chan struct{}
	waitResult Result
	supervised chan struct{}

	waitOnce sync.Once
	result   Result
}

// rootOps are the operating-system effects on one root. Tests replace them to
// script the order of exit, reap and signals.
type rootOps struct {
	// awaitExit blocks until the root exited and reports its result. held
	// reports that the root is still an unreaped zombie, to be released with
	// reap; otherwise it was reaped at exit.
	awaitExit func() (result Result, held bool)
	// waitable reports, without blocking, that a held root can be reaped at
	// once (the whole process exited), from an authoritative source. false
	// with a nil error is retried; an error means waitability can never be
	// established, which ends finalization with ErrReleaseUnavailable.
	waitable func() (bool, error)
	// reap releases a held root's PID. It fails only when the root could not
	// be reaped; an unsuccessful exit status is not a reap failure.
	reap      func() error
	interrupt func() error
	killTree  func() error
	// signalsByNumber reports that interrupt and killTree address the root
	// by PID/PGID, so they must not run once the reap released that number.
	signalsByNumber bool
	// lockHook, set only by tests, runs when signalMu was found held right
	// before it is acquired (see noteContention).
	lockHook func(point string)
}

// Bounds of the poll for root exit where it cannot be awaited.
const (
	exitPollMin = 10 * time.Millisecond
	exitPollMax = time.Second
)

// nextPollDelay doubles a poll delay up to exitPollMax.
func nextPollDelay(delay time.Duration) time.Duration {
	return min(2*delay, exitPollMax)
}

// ErrSignalsFinalized is returned, without a syscall, by Interrupt and
// KillTree once the process was finalized (or, where the root cannot be held,
// once it was reaped at exit). It wraps os.ErrProcessDone, so callers that
// treat that as done keep working; it is not evidence that a signal was
// delivered or that the group is gone, so termination accounting must not
// count it as a successful kill.
var ErrSignalsFinalized = fmt.Errorf("owned process was finalized; no further signals are sent: %w", os.ErrProcessDone)

// ErrReleaseUnavailable is Finalize's terminal error when the exited root
// could not be confirmed waitable without blocking: it stays unreaped (and
// counted), and signals stay refused.
var ErrReleaseUnavailable = errors.New("owned root process cannot be released: waitability is unavailable")

// unfinalized counts ExecLauncher processes whose release is not established.
var unfinalized atomic.Int64

// UnfinalizedProcesses reports how many processes started by ExecLauncher
// have no established release: never finalized, still finalizing (root
// alive), ErrReleaseUnavailable, or a failed reap. Every owner path
// must end in Finalize; tests use this as a leak check.
func UnfinalizedProcesses() int { return int(unfinalized.Load()) }

func newExecProcess(pid int, ops rootOps, owned *ownedResources, stdin io.WriteCloser, stdout, stderr io.ReadCloser) *execProcess {
	return &execProcess{
		pid:        pid,
		ops:        ops,
		owned:      owned,
		stdin:      &onceWriteCloser{WriteCloser: stdin},
		stdout:     newTrackedReadCloser(stdout),
		stderr:     newTrackedReadCloser(stderr),
		rootExited: make(chan struct{}),
		released:   make(chan struct{}),
		supervised: make(chan struct{}),
		finalDone:  make(chan struct{}),
	}
}

// superviseRoot publishes the root's exit as soon as it is observed and lets
// Wait return once both output pipes ended. It never reaps a held root.
func (p *execProcess) superviseRoot() {
	defer close(p.supervised)
	observed, held := p.ops.awaitExit()
	if !held {
		p.endGroupSignals()
	}
	p.rootResult = observed
	p.held = held
	close(p.rootExited)

	<-p.stdout.done
	<-p.stderr.done
	p.waitResult = observed
	close(p.released)
}

// Finalize declares that the owner sends no more signals; it never kills
// (call KillTree first). The first call starts the finalization worker, which
// owns the commit and reap from then on, also after every caller stopped
// waiting. Finalize waits for its outcome, bounded by ctx: nil once the root
// was released; the reap's error or ErrReleaseUnavailable otherwise (signals
// stay refused); ctx's error while it is still in progress. Before the root
// exited nothing is committed and signals still pass. It is idempotent and
// safe to call concurrently.
func (p *execProcess) Finalize(ctx context.Context) error {
	p.finalizeOnce.Do(func() { go p.finalizeWorker() })
	select {
	case <-p.finalDone:
		return p.finalErr
	default:
	}
	select {
	case <-p.finalDone:
		return p.finalErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// finalizeWorker waits for the root's exit, commits (refusing every later
// group signal), and reaps a held root once it is waitable. Every outcome is
// terminal and returned to every Finalize caller; only a successful reap
// uncounts the process.
func (p *execProcess) finalizeWorker() {
	<-p.rootExited
	p.noteContention(hookCommit)
	_ = lockscope.WithMutexLock(&p.signalMu, func() error {
		p.reaped = true
		return nil
	})
	if p.held {
		p.finalErr = p.reapWhenWaitable()
	}
	if p.counted && p.finalErr == nil {
		unfinalized.Add(-1)
	}
	close(p.finalDone)
}

// reapWhenWaitable reaps a held root once it is authoritatively waitable,
// retrying a negative answer with backoff. When waitability can never be
// established (the query fails, for example Linux without waitid) the root is
// left unreaped, which is the terminal outcome ErrReleaseUnavailable.
func (p *execProcess) reapWhenWaitable() error {
	for delay := exitPollMin; ; delay = nextPollDelay(delay) {
		waitable, err := p.ops.waitable()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrReleaseUnavailable, err)
		}
		if waitable {
			return p.ops.reap()
		}
		time.Sleep(delay)
	}
}

// endGroupSignals refuses group signals once a root was reaped at exit (the
// platform cannot hold it).
func (p *execProcess) endGroupSignals() {
	_ = lockscope.WithMutexLock(&p.signalMu, func() error {
		p.reaped = true
		return nil
	})
}

// waitRoot reaps the root with exec.Cmd.Wait. With *os.File stdio it returns
// as soon as the root exited, regardless of descendants that still hold the
// pipes, and it closes no parent pipe end.
func waitRoot(cmd *exec.Cmd) Result {
	err := cmd.Wait()
	result := Result{ExitCode: -1, Err: err, ExitedAt: time.Now()}
	if state := cmd.ProcessState; state != nil {
		result.ExitCode = state.ExitCode()
		result.Signal = processSignal(state)
	}
	return result
}

// reapExited reaps a root whose exit was already observed. exec.Cmd.Wait's
// *exec.ExitError only repeats that exit; without a ProcessState the reap
// itself failed.
func reapExited(cmd *exec.Cmd) error {
	err := cmd.Wait()
	if cmd.ProcessState != nil {
		return nil
	}
	return err
}

// signal sends one group signal unless the reap already released the number
// it addresses.
func (p *execProcess) signal(send func() error) error {
	if !p.ops.signalsByNumber {
		return send()
	}
	p.noteContention(hookSignal)
	return lockscope.WithMutexLock(&p.signalMu, func() error {
		if p.reaped {
			return ErrSignalsFinalized
		}
		return send()
	})
}

// Points at which tests synchronize on a contended signalMu.
const (
	hookSignal = "signal"
	hookCommit = "commit"
)

// noteContention lets a test observe that point found signalMu held by a
// competitor (TryLock failed) right before acquiring it. Production sets no
// hook and skips the probe.
func (p *execProcess) noteContention(point string) {
	if p.ops.lockHook == nil {
		return
	}
	if p.signalMu.TryLock() {
		p.signalMu.Unlock()
		return
	}
	p.ops.lockHook(point)
}

// RootResult is the root's exit status. It is valid once RootExited closed;
// ReleaseErr stays empty because owned resources are released only by Wait.
func (p *execProcess) RootResult() Result {
	<-p.rootExited
	return p.rootResult
}

// RootExited closes when the root process has exited, even while a
// descendant keeps stdout or stderr open. On Linux and Darwin the root is
// not reaped yet, so its process group can still be signalled.
func (p *execProcess) RootExited() <-chan struct{} { return p.rootExited }

type trackedReadCloser struct {
	io.ReadCloser
	done      chan struct{}
	once      sync.Once
	closeOnce sync.Once
	closeErr  error
}

func newTrackedReadCloser(reader io.ReadCloser) *trackedReadCloser {
	return &trackedReadCloser{ReadCloser: reader, done: make(chan struct{})}
}

func (r *trackedReadCloser) Read(buffer []byte) (int, error) {
	n, err := r.ReadCloser.Read(buffer)
	if err != nil {
		r.once.Do(func() { close(r.done) })
	}
	return n, err
}

func (r *trackedReadCloser) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.ReadCloser.Close(); r.once.Do(func() { close(r.done) }) })
	return r.closeErr
}

func (p *execProcess) PID() int                { return p.pid }
func (p *execProcess) Stdin() io.WriteCloser   { return p.stdin }
func (p *execProcess) Stdout() io.ReadCloser   { return p.stdout }
func (p *execProcess) Stderr() io.ReadCloser   { return p.stderr }
func (p *execProcess) Interrupt() error        { return p.signal(p.ops.interrupt) }
func (p *execProcess) KillTree() error         { return p.signal(p.ops.killTree) }
func (p *execProcess) TreeKillSupported() bool { return ownedTreeKillSupported() }

// Wait returns once both output pipes reached EOF or were closed and the
// root exited. It does not mean the root was reaped: on Linux and Darwin
// that happens only in Finalize. Descendants that retain either pipe keep
// Wait blocked, which is why adapters observe RootExited and use KillTree
// before claiming quiescence. Afterwards it releases the parent's pipe ends
// and the owned resources (on Windows, the kill-on-close job).
func (p *execProcess) Wait() Result {
	p.waitOnce.Do(func() {
		<-p.released
		_ = p.stdout.Close()
		_ = p.stderr.Close()
		_ = p.stdin.Close()
		result := p.waitResult
		result.ReleaseErr = p.owned.close()
		p.result = result
	})
	return p.result
}

// onceWriteCloser closes stdin once and reports the first result to every
// caller, like exec.Cmd's StdinPipe, so a later Close after Wait is not an
// error.
type onceWriteCloser struct {
	io.WriteCloser
	once sync.Once
	err  error
}

func (w *onceWriteCloser) Close() error {
	w.once.Do(func() { w.err = w.WriteCloser.Close() })
	return w.err
}
