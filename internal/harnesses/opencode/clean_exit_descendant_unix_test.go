//go:build unix

package opencode

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const (
	cleanDescendantHelperEnv = "CONDUIT_OPENCODE_CLEAN_DESCENDANT_HELPER"
	cleanDescendantPIDFile   = "CONDUIT_OPENCODE_CLEAN_DESCENDANT_PIDFILE"
	cleanDescendantFIFOEnv   = "CONDUIT_OPENCODE_CLEAN_DESCENDANT_FIFO"
)

// TestCleanExitDescendantHelper is a subprocess helper for
// TestCleanRunExitKillsDetachedPipeDescendant. It spawns a descendant in the
// same process group with stdin/stdout/stderr redirected to /dev/null (holding
// none of the run client's stdio pipes) and fd 3 open on the test's liveness
// FIFO, records the descendant's PID, and exits cleanly with status 0.
func TestCleanExitDescendantHelper(t *testing.T) {
	if os.Getenv(cleanDescendantHelperEnv) != "1" {
		return
	}
	pidFile := os.Getenv(cleanDescendantPIDFile)
	fifoPath := os.Getenv(cleanDescendantFIFOEnv)
	cmd := exec.Command("sh", "-c", `exec 3>"$1"; sleep 30 </dev/null >/dev/null 2>&1 & echo $!`, "_", fifoPath)
	out, err := cmd.Output()
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(pidFile, out, 0o600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

type cleanDescendantLauncher struct {
	pidFile  string
	fifoPath string
}

func (l cleanDescendantLauncher) Start(spec processruntime.Spec) (processruntime.Process, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return (processruntime.ExecLauncher{}).Start(processruntime.Spec{
		Path: exe,
		Args: []string{"-test.run=^TestCleanExitDescendantHelper$"},
		Dir:  spec.Dir,
		Env: []string{
			cleanDescendantHelperEnv + "=1",
			cleanDescendantPIDFile + "=" + l.pidFile,
			cleanDescendantFIFOEnv + "=" + l.fifoPath,
		},
	})
}

// Decision 2026-11-20 (Option C): when an `opencode run` root exits cleanly
// (code 0), readRun still terminates the run client's own process group
// before finalization, killing descendants that closed all stdio pipes
// (`sleep 30 </dev/null >/dev/null 2>&1 &`). Liveness is observed via an
// unrelated FIFO fd that the kernel closes on process death (distinguishing
// a dead zombie from a live process), and cleanup never signals a PID that
// was already observed dead.
func TestCleanRunExitKillsDetachedPipeDescendant(t *testing.T) {
	if !processruntime.RootRetentionSupported() {
		t.Skip("platform does not support root retention")
	}
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "descendant.pid")
	fifoPath := filepath.Join(tmp, "alive.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	fifoDone := make(chan struct{})
	// Open the read end in a goroutine before launch so the helper's
	// O_WRONLY open unblocks; EOF proves the descendant closed fd 3
	// (died, even if it is still an unreaped zombie).
	go func() {
		defer close(fifoDone)
		f, err := os.OpenFile(fifoPath, os.O_RDONLY, 0)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = io.Copy(io.Discard, f)
	}()

	// Register cleanup before starting any child process. Never signal a PID
	// that was already observed dead through FIFO EOF.
	t.Cleanup(func() {
		select {
		case <-fifoDone:
			return
		default:
		}
		if raw, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		// Unblock the FIFO reader if the helper never opened the write end.
		if rw, err := os.OpenFile(fifoPath, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
			_ = rw.Close()
		}
		<-fifoDone
	})

	adapter := New(withFakeServer(Config{
		Control:   &fakeControl{},
		Launcher:  cleanDescendantLauncher{pidFile: pidFile, fifoPath: fifoPath},
		StopGrace: 2 * time.Second,
	}))
	req := openRequest()
	req.Workspace.Path = tmp
	opened, err := adapter.OpenSession(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Session.StartTurn(context.Background(), provider.StartTurnRequest{
		Session:        opened.Session.Ref(),
		Assignment:     req.Assignment,
		ResultContract: req.ResultContract,
	}); err != nil {
		t.Fatal(err)
	}
	awaitTurnEnded(t, opened.Session)

	select {
	case <-fifoDone:
	case <-time.After(3 * time.Second):
		t.Fatal("descendant survived clean run exit (liveness FIFO still open)")
	}

	res := awaitCloseOutcome(t, closeAsync(opened.Session), 3*time.Second)
	if res.ExecutionState != provider.ExecutionNone || !res.ResourcesReleased {
		t.Fatalf("Close after clean run exit = %#v, want ExecutionNone and ResourcesReleased=true", res)
	}
}
