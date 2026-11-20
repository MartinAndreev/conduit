package memoryinfra

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/process"
)

// stuckProcess never exits on its own and refuses tree termination; release
// lets the test end its reap. Like execProcess, Wait also waits until both
// output pipes reached EOF or were closed by the parent.
type stuckProcess struct {
	stdin            *io.PipeWriter
	stdout, stderr   *trackedPipe
	stdoutW, stderrW *io.PipeWriter
	release          chan struct{}
}

// trackedPipe is the parent's end of an output pipe; done closes on EOF, a
// read error or Close, which also unblocks a pending Read.
type trackedPipe struct {
	*io.PipeReader
	done chan struct{}
	once sync.Once
}

func newTrackedPipe(reader *io.PipeReader) *trackedPipe {
	return &trackedPipe{PipeReader: reader, done: make(chan struct{})}
}

func (p *trackedPipe) Read(buffer []byte) (int, error) {
	n, err := p.PipeReader.Read(buffer)
	if err != nil {
		p.once.Do(func() { close(p.done) })
	}
	return n, err
}

func (p *trackedPipe) Close() error {
	err := p.PipeReader.Close()
	p.once.Do(func() { close(p.done) })
	return err
}

func newStuckProcess() *stuckProcess {
	stdinR, stdinW := io.Pipe()
	go func() { _, _ = io.Copy(io.Discard, stdinR) }()
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	return &stuckProcess{stdin: stdinW, stdout: newTrackedPipe(stdoutR), stderr: newTrackedPipe(stderrR), stdoutW: stdoutW, stderrW: stderrW, release: make(chan struct{})}
}

func (p *stuckProcess) PID() int                { return 1 }
func (p *stuckProcess) Stdin() io.WriteCloser   { return p.stdin }
func (p *stuckProcess) Stdout() io.ReadCloser   { return p.stdout }
func (p *stuckProcess) Stderr() io.ReadCloser   { return p.stderr }
func (p *stuckProcess) Interrupt() error        { return nil }
func (p *stuckProcess) KillTree() error         { return errors.New("kill refused") }
func (p *stuckProcess) TreeKillSupported() bool { return true }
func (p *stuckProcess) Wait() process.Result {
	p.waitOutput()
	return process.Result{ExitCode: -1, Signal: "killed"}
}

// waitOutput waits for release and both output pipes, as execProcess.Wait
// does.
func (p *stuckProcess) waitOutput() {
	<-p.release
	<-p.stdout.done
	<-p.stderr.done
}

type fixedLauncher struct {
	child process.Process
	err   error
}

func (l fixedLauncher) Start(process.Spec) (process.Process, error) { return l.child, l.err }

// A git process whose cleanup is not confirmed within the budget fails the
// call with the cancellation and a GitCleanup that keeps observing it.
func TestUnconfirmedCleanupIsReported(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	child := newStuckProcess()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := NewWorkspaceSnapshots(fixedLauncher{child: child}).ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go"})
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("returned after %v", elapsed)
	}
	var cleanup *GitCleanup
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrGitCleanup) || !errors.As(err, &cleanup) {
		t.Fatalf("err = %v", err)
	}
	if state := cleanup.State(); state.Complete || state.TreeTerminationErr == nil {
		t.Fatalf("state = %+v", state)
	}
	close(child.release)
	select {
	case <-cleanup.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup never completed")
	}
	if !cleanup.State().Complete {
		t.Fatal("cleanup state not complete")
	}
}

// The same unconfirmed cleanup after git finished on its own is reported
// without a cancellation.
func TestUnconfirmedCleanupAfterExit(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	child := newStuckProcess()
	_ = child.stdoutW.Close()
	_ = child.stderrW.Close()
	launcher := exitedLauncher{stuck: child}
	_, err := NewWorkspaceSnapshots(launcher).ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go"})
	var cleanup *GitCleanup
	if !errors.Is(err, ErrGitCleanup) || errors.Is(err, context.Canceled) || !errors.As(err, &cleanup) {
		t.Fatalf("err = %v", err)
	}
	if state := cleanup.State(); state.TreeTerminationRequested || state.RootReaped {
		t.Fatalf("state = %+v", state)
	}
	close(child.release)
	select {
	case <-cleanup.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("reap never completed")
	}
	if state := cleanup.State(); !state.RootReaped || state.TreeTerminationRequested {
		t.Fatalf("state = %+v", state)
	}
}

// exitedLauncher returns a process whose root already exited but whose
// release (Wait) is stuck.
type exitedLauncher struct{ stuck *stuckProcess }

func (l exitedLauncher) Start(process.Spec) (process.Process, error) {
	return exitedProcess{l.stuck}, nil
}

type exitedProcess struct{ *stuckProcess }

func (p exitedProcess) RootExited() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
func (p exitedProcess) RootResult() process.Result { return process.Result{} }

func TestStartFailureIsOpaque(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	cause := errors.New("chdir " + dir + ": denied")
	_, err := NewWorkspaceSnapshots(fixedLauncher{err: cause}).ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go"})
	if !errors.Is(err, ErrGitUnavailable) || !errors.Is(err, cause) || strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v", err)
	}
}

// scriptedProcess exits at once with exitCode after printing stdout and
// counts tree kills and finalizations; finalizeErr is a failed release.
type scriptedProcess struct {
	*stuckProcess
	exitCode    int
	kills       *atomic.Int32
	finalizes   *atomic.Int32
	finalizeErr error
}

func newScriptedProcess(stdout string, exitCode int) scriptedProcess {
	child := newStuckProcess()
	close(child.release)
	go func() {
		_, _ = io.WriteString(child.stdoutW, stdout)
		_ = child.stdoutW.Close()
		_ = child.stderrW.Close()
	}()
	return scriptedProcess{stuckProcess: child, exitCode: exitCode, kills: &atomic.Int32{}, finalizes: &atomic.Int32{}}
}

// Finalize implements process.Finalizer: the scripted root has exited.
func (p scriptedProcess) Finalize(context.Context) error {
	p.finalizes.Add(1)
	return p.finalizeErr
}

func (p scriptedProcess) KillTree() error {
	p.kills.Add(1)
	return nil
}

func (p scriptedProcess) Wait() process.Result {
	p.waitOutput()
	return process.Result{ExitCode: p.exitCode}
}

// scriptedLauncher hands out scripted processes in order and counts starts.
type scriptedLauncher struct {
	children []scriptedProcess
	starts   atomic.Int32
}

func (l *scriptedLauncher) Start(process.Spec) (process.Process, error) {
	next := int(l.starts.Add(1)) - 1
	if next >= len(l.children) {
		return nil, errors.New("unexpected start")
	}
	return l.children[next], nil
}

// A git process that finished on its own is finalized (released) and never
// signalled.
func TestFinishedGitIsNotSignalled(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	hash := strings.Repeat("a", 40)
	child := newScriptedProcess(hash+"\n", 0)
	launcher := &scriptedLauncher{children: []scriptedProcess{child}}
	got := observe(t, NewWorkspaceSnapshots(launcher), dir, "a.go")
	if got[0] != presentAs(hash) {
		t.Fatalf("got %v", got)
	}
	if kills, finalizes := child.kills.Load(), child.finalizes.Load(); kills != 0 || finalizes != 1 {
		t.Fatalf("KillTree called %d times, Finalize %d times", kills, finalizes)
	}
}

// A finished git whose release (finalization) fails fails the call with a
// completed GitCleanup that carries the release error; nothing is signalled.
func TestFinishedGitReleaseFailureIsAnError(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	child := newScriptedProcess(strings.Repeat("a", 40)+"\n", 0)
	reapErr := errors.New("reap failed")
	child.finalizeErr = reapErr
	launcher := &scriptedLauncher{children: []scriptedProcess{child}}
	observed, err := NewWorkspaceSnapshots(launcher).ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go"})
	var cleanup *GitCleanup
	if !errors.Is(err, ErrGitCleanup) || !errors.Is(err, reapErr) || !errors.As(err, &cleanup) || observed != nil {
		t.Fatalf("observed %v, err %v", observed, err)
	}
	if state := cleanup.State(); !state.Complete || state.RootReaped || state.TreeTerminationRequested || !errors.Is(state.RootResult.ReleaseErr, reapErr) {
		t.Fatalf("state = %+v", state)
	}
	if kills := child.kills.Load(); kills != 0 {
		t.Fatalf("KillTree called %d times", kills)
	}
}

// A git run longer than the cleanup budget (a slow clean or LFS filter) that
// then finishes and releases quickly succeeds: the budget measures only the
// cleanup after git finished, not git's own run.
func TestSlowGitRunDoesNotSpendCleanupBudget(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	hash := strings.Repeat("a", 40)
	child := scriptedProcess{stuckProcess: newStuckProcess(), kills: &atomic.Int32{}, finalizes: &atomic.Int32{}}
	reader := NewWorkspaceSnapshots(&scriptedLauncher{children: []scriptedProcess{child}})
	reader.cleanupBudget = 50 * time.Millisecond
	go func() {
		time.Sleep(4 * reader.cleanupBudget)
		_, _ = io.WriteString(child.stdoutW, hash+"\n")
		_ = child.stdoutW.Close()
		_ = child.stderrW.Close()
		close(child.release)
	}()
	observed, err := reader.ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(observed) != 1 || observed[0] != presentAs(hash) {
		t.Fatalf("observed %v", observed)
	}
	if kills, finalizes := child.kills.Load(), child.finalizes.Load(); kills != 0 || finalizes != 1 {
		t.Fatalf("KillTree called %d times, Finalize %d times", kills, finalizes)
	}
}

// Git that exits 0 without one valid hash per path, or with more output
// than the bound allows, fails the call: nothing from that run is trusted,
// nothing is marked present, and there is no control run or retry.
func TestMalformedGitOutputIsAnError(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	write(t, dir, "b.go", "b\n")
	hash := strings.Repeat("a", 40) + "\n"
	for _, stdout := range []string{"", "not-a-hash\n", hash, strings.Repeat(hash, 50)} {
		launcher := &scriptedLauncher{children: []scriptedProcess{newScriptedProcess(stdout, 0), newScriptedProcess("", 0)}}
		observed, err := NewWorkspaceSnapshots(launcher).ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go", "b.go"})
		if !errors.Is(err, ErrGitOutput) || observed != nil {
			t.Fatalf("%d bytes: observed %v, err %v", len(stdout), observed, err)
		}
		if starts := launcher.starts.Load(); starts != 1 {
			t.Fatalf("%d bytes: started %d processes", len(stdout), starts)
		}
	}
}

// exitedOpenProcess has exited, but a descendant still holds stdout, so its
// pipes have not ended. Like production on Linux and Darwin its root stays
// unreaped until Finalize, so its group can still be killed. It counts tree
// kills and finalizations, and records a finalization before any kill.
type exitedOpenProcess struct {
	*stuckProcess
	kills               *atomic.Int32
	finalizes           *atomic.Int32
	finalizedBeforeKill *atomic.Bool
}

func newExitedOpenProcess(stuck *stuckProcess) exitedOpenProcess {
	return exitedOpenProcess{stuckProcess: stuck, kills: &atomic.Int32{}, finalizes: &atomic.Int32{}, finalizedBeforeKill: &atomic.Bool{}}
}

func (p exitedOpenProcess) Finalize(context.Context) error {
	if p.finalizes.Add(1) == 1 && p.kills.Load() == 0 {
		p.finalizedBeforeKill.Store(true)
	}
	return nil
}

func (p exitedOpenProcess) RootExited() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
func (p exitedOpenProcess) RootResult() process.Result { return process.Result{} }
func (p exitedOpenProcess) KillTree() error {
	p.kills.Add(1)
	return nil
}

// Cancellation that finds the root already exited still kills the group (the
// unreaped root keeps its number reserved), registers finalization after the
// kill, closes the parent's pipe ends, and fails with the cancellation only.
func TestCancellationAfterRootExitKillsAndFinalizes(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	stuck := newStuckProcess()
	close(stuck.release)
	_ = stuck.stderrW.Close()
	defer stuck.stdoutW.Close()
	child := newExitedOpenProcess(stuck)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	observed, err := NewWorkspaceSnapshots(fixedLauncher{child: child}).ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go"})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrGitCleanup) || observed != nil {
		t.Fatalf("observed %v, err %v", observed, err)
	}
	if kills, finalizes := child.kills.Load(), child.finalizes.Load(); kills != 1 || finalizes == 0 || child.finalizedBeforeKill.Load() {
		t.Fatalf("KillTree called %d times, Finalize %d times (before the kill: %v)", kills, finalizes, child.finalizedBeforeKill.Load())
	}
	// The writer side is still open: the parent's end was closed by the
	// reader, not by the test.
	select {
	case <-stuck.stdout.done:
	default:
		t.Fatal("parent stdout end was not closed")
	}
}

// Cancellation during the filesystem phase is an error even when no path is
// admitted and git is never needed.
func TestCancellationDuringFilesystemPhase(t *testing.T) {
	dir := workspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := NewWorkspaceSnapshots(nil)
	lstat := reader.files.lstat
	reader.files.lstat = func(name string) (fs.FileInfo, error) {
		if filepath.Base(name) == "last.go" {
			cancel()
		}
		return lstat(name)
	}
	observed, err := reader.ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"first.go", "last.go"})
	if !errors.Is(err, context.Canceled) || observed != nil {
		t.Fatalf("observed %v, err %v", observed, err)
	}
}

// A root found exited after cancellation whose cleanup is not confirmed in
// the budget reports both the cancellation and the pending cleanup; the
// owner's single kill was sent.
func TestCancellationAfterRootExitWithStuckReap(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	stuck := newStuckProcess()
	_ = stuck.stderrW.Close()
	defer stuck.stdoutW.Close()
	child := newExitedOpenProcess(stuck)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := NewWorkspaceSnapshots(fixedLauncher{child: child}).ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go"})
	var cleanup *GitCleanup
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &cleanup) || child.kills.Load() != 1 {
		t.Fatalf("err = %v, kills %d", err, child.kills.Load())
	}
	if !strings.Contains(err.Error(), ErrGitCleanup.Error()) {
		t.Fatalf("text = %q", err.Error())
	}
	close(stuck.release)
	select {
	case <-cleanup.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("reap never completed")
	}
}

// Cancellation between paths stops the filesystem phase.
func TestCancellationBetweenPaths(t *testing.T) {
	dir := workspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := NewWorkspaceSnapshots(nil)
	lstat, calls := reader.files.lstat, 0
	reader.files.lstat = func(name string) (fs.FileInfo, error) {
		calls++
		cancel()
		return lstat(name)
	}
	_, err := reader.ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go", "b.go", "c.go"})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err %v after %d lookups", err, calls)
	}
}

// A start failure in the control run or in a per-file retry fails the call.
func TestFailureAfterAFailedBatch(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	write(t, dir, "b.go", "b\n")
	control := strings.Repeat("e", 40) + "\n"
	for name, children := range map[string][]scriptedProcess{
		"control": {newScriptedProcess("", 1)},
		"retry":   {newScriptedProcess("", 1), newScriptedProcess(control, 0)},
	} {
		launcher := &scriptedLauncher{children: children}
		observed, err := NewWorkspaceSnapshots(launcher).ObserveFiles(context.Background(), compose.SnapshotRef(dir), []string{"a.go", "b.go"})
		if !errors.Is(err, ErrGitUnavailable) || observed != nil {
			t.Fatalf("%s: observed %v, err %v", name, observed, err)
		}
	}
}

// slowClosePipe is a parent pipe end whose Close blocks until gate opens and
// then reports err.
type slowClosePipe struct {
	*trackedPipe
	gate <-chan struct{}
	err  error
}

func (p slowClosePipe) Close() error {
	<-p.gate
	_ = p.trackedPipe.Close()
	return p.err
}

type slowCloseProcess struct {
	exitedOpenProcess
	stdout slowClosePipe
}

func (p slowCloseProcess) Stdout() io.ReadCloser { return p.stdout }

// A pipe close that outlasts the budget leaves the observer with the whole
// remaining release; after Done its state keeps the close error and is
// complete.
func TestSlowPipeCloseIsObservable(t *testing.T) {
	requireGit(t)
	dir := workspace(t)
	write(t, dir, "a.go", "a\n")
	stuck := newStuckProcess()
	close(stuck.release)
	_ = stuck.stderrW.Close()
	defer stuck.stdoutW.Close()
	gate, closeErr := make(chan struct{}), errors.New("close failed late")
	child := slowCloseProcess{
		exitedOpenProcess: newExitedOpenProcess(stuck),
		stdout:            slowClosePipe{trackedPipe: stuck.stdout, gate: gate, err: closeErr},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := NewWorkspaceSnapshots(fixedLauncher{child: child}).ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.go"})
	var cleanup *GitCleanup
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &cleanup) || child.kills.Load() != 1 {
		t.Fatalf("err = %v, kills %d", err, child.kills.Load())
	}
	if state := cleanup.State(); state.Complete || state.StdoutClosure.Complete || !state.TreeTerminationRequested {
		t.Fatalf("state before the close = %+v", state)
	}
	close(gate)
	select {
	case <-cleanup.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("release never completed")
	}
	state := cleanup.State()
	if !state.Complete || !state.RootReaped || !state.StdoutClosure.Complete || !errors.Is(state.StdoutClosure.Err, closeErr) ||
		!state.StderrClosure.Complete || state.StderrClosure.Err != nil || !errors.Is(state.PipeCloseErr, closeErr) || state.PipesClosed {
		t.Fatalf("state after Done = %+v", state)
	}
}
