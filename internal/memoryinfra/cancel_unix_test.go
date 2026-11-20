//go:build !windows

package memoryinfra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/process"
)

// Cancelling while git runs a clean filter kills git and the filter and
// reaps git before ObserveFiles returns.
func TestCancellationKillsAndReapsGit(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	pidFile := filepath.Join(workspace(t), "filter.pid")
	script := filepath.Join(workspace(t), "slow-filter.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho $$ > '"+pidFile+"'\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "config", "filter.slow.clean", script)
	write(t, dir, ".gitattributes", "*.slow filter=slow\n")
	write(t, dir, "a.slow", "a\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(pidFile); err == nil {
				break
			}
		}
		cancel()
	}()
	launcher := &countingLauncher{}
	started := time.Now()
	observed, err := NewWorkspaceSnapshots(launcher).ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.slow"})
	if !errors.Is(err, context.Canceled) || observed != nil {
		t.Fatalf("observed %v, err %v", observed, err)
	}
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("returned after %v", elapsed)
	}
	if launcher.starts() != 1 {
		t.Fatalf("started %d git processes", launcher.starts())
	}
	child := launcher.started[0]
	waited := make(chan struct{})
	go func() {
		child.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("git was not reaped")
	}
	if result := child.Wait(); result.Signal == "" && result.ExitCode == 0 {
		t.Fatalf("git was not killed: %+v", result)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("filter never started: %v", err)
	}
	filterPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(filterPID, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(filterPID, syscall.SIGKILL)
			t.Fatal("the filter process outlived the cancelled call")
		}
	}
}

// killCounting wraps a real process, counting tree kills and forwarding the
// root-exit observer and finalization (a decorator that dropped Finalize
// would leave the root unreleased).
type killCounting struct {
	process.Process
	observer process.RootExitObserver
	kills    *atomic.Int32
}

var (
	_ process.Finalizer        = killCounting{}
	_ process.RootExitObserver = killCounting{}
)

func (p killCounting) Finalize(ctx context.Context) error {
	return process.Finalize(ctx, p.Process)
}

func (p killCounting) KillTree() error {
	p.kills.Add(1)
	return p.Process.KillTree()
}
func (p killCounting) RootExited() <-chan struct{} { return p.observer.RootExited() }
func (p killCounting) RootResult() process.Result  { return p.observer.RootResult() }

type killCountingLauncher struct {
	kills   atomic.Int32
	started chan process.Process
}

func (l *killCountingLauncher) Start(spec process.Spec) (process.Process, error) {
	child, err := process.ExecLauncher{}.Start(spec)
	if err != nil {
		return nil, err
	}
	l.started <- child
	return killCounting{Process: child, observer: child.(process.RootExitObserver), kills: &l.kills}, nil
}

// Git exits while a filter's background child still holds git's stderr; the
// cancelled call kills the group of the exited (still unreaped) root, which
// reaches that child, closes the parent's pipe ends, and returns promptly
// with the drain and the root's release finished.
func TestCancellationAfterExitWithDescendantHoldingOutput(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	pidFile := filepath.Join(workspace(t), "sleeper.pid")
	script := filepath.Join(workspace(t), "holding-filter.sh")
	body := "#!/bin/sh\nsleep 30 </dev/null >/dev/null &\necho $! > '" + pidFile + "'\nexec cat\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "config", "filter.hold.clean", script)
	write(t, dir, ".gitattributes", "*.hold filter=hold\n")
	write(t, dir, "a.hold", "a\n")
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	launcher := &killCountingLauncher{started: make(chan process.Process, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel only once git's root exited, while the sleeper holds stderr.
	child := make(chan process.Process, 1)
	go func() {
		started := <-launcher.started
		child <- started
		<-started.(process.RootExitObserver).RootExited()
		cancel()
	}()
	started := time.Now()
	observed, err := NewWorkspaceSnapshots(launcher).ObserveFiles(ctx, compose.SnapshotRef(dir), []string{"a.hold"})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrGitCleanup) || observed != nil {
		t.Fatalf("observed %v, err %v", observed, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("returned after %v", elapsed)
	}
	if kills := launcher.kills.Load(); kills != 1 {
		t.Fatalf("KillTree called %d times", kills)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("filter never started its background child: %v", err)
	}
	sleeperPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(sleeperPID, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the group kill did not reach the background child")
		}
	}
	// Wait needs the root's exit and both pipes ended: it returns at once
	// only if the drain finished.
	waited := make(chan process.Result, 1)
	go func() { waited <- (<-child).Wait() }()
	select {
	case result := <-waited:
		if result.ExitCode != 0 {
			t.Fatalf("git result %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("drain or reap still blocked by the descendant")
	}
}
