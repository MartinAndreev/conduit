//go:build unix

package opencode

import (
	"context"
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
)

// TestCleanExitDescendantHelper is a subprocess helper for
// TestCleanRunExitKillsDetachedPipeDescendant. It spawns a descendant in the
// same process group with all stdio redirected to /dev/null (holding no pipe),
// records the descendant's PID to a file, and exits cleanly with status 0.
func TestCleanExitDescendantHelper(t *testing.T) {
	if os.Getenv(cleanDescendantHelperEnv) != "1" {
		return
	}
	pidFile := os.Getenv(cleanDescendantPIDFile)
	cmd := exec.Command("sh", "-c", "sleep 30 </dev/null >/dev/null 2>&1 & echo $!")
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
	pidFile string
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
		},
	})
}

// Decision 2026-11-20 (Option C): when an `opencode run` root exits cleanly
// (code 0), readRun still terminates the run client's own process group
// before finalization, killing descendants that closed all pipes
// (`sleep 30 </dev/null >/dev/null 2>&1 &`). Close after the turn ended
// confirms ResourcesReleased=true.
func TestCleanRunExitKillsDetachedPipeDescendant(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	adapter := New(withFakeServer(Config{
		Control:   &fakeControl{},
		Launcher:  cleanDescendantLauncher{pidFile: pidFile},
		StopGrace: 2 * time.Second,
	}))
	req := openRequest()
	req.Workspace.Path = t.TempDir()
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

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read descendant pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid descendant pid %q: %v", string(raw), err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})

	// Wait for the OS to finish reaping/removing the killed sleep process.
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant pid %d survived clean run exit (kill(0) = %v)", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	res := awaitCloseOutcome(t, closeAsync(opened.Session), 3*time.Second)
	if res.ExecutionState != provider.ExecutionNone || !res.ResourcesReleased {
		t.Fatalf("Close after clean run exit = %#v, want ExecutionNone and ResourcesReleased=true", res)
	}
}
