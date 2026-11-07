//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOwnedProcessGroupKillsAndReapsDescendant(t *testing.T) {
	proc, err := (ExecLauncher{}).Start(Spec{
		Path: os.Args[0], Args: []string{"-test.run=TestOwnedProcessHelper", "--", "descendant"},
		Env: []string{"CONDUIT_PROCESS_HELPER=1"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !proc.TreeKillSupported() {
		t.Fatal("Unix process does not report process-group termination support")
	}
	go io.Copy(io.Discard, proc.Stderr())
	reader := bufio.NewReader(proc.Stdout())
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse descendant PID %q: %v", line, err)
	}
	go io.Copy(io.Discard, reader)
	if err := proc.KillTree(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("KillTree: %v", err)
	}
	done := make(chan Result, 1)
	go func() { done <- proc.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owned process group was not reaped")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(childPID, 0)
		if err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("descendant %d still exists after process-group kill", childPID)
}

func TestOwnedProcessHelper(t *testing.T) {
	if os.Getenv("CONDUIT_PROCESS_HELPER") != "1" {
		return
	}
	child := exec.Command("/bin/sh", "-c", "sleep 60")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(child.Process.Pid)
	if slices.Contains(os.Args, "orphan") {
		// The root exits at once; the descendant keeps stdout and stderr.
		os.Exit(0)
	}
	_ = child.Wait()
	os.Exit(0)
}

func TestReplaceEnvRemovesInheritedVariables(t *testing.T) {
	t.Setenv("CONDUIT_PROCESS_PARENT_ONLY", "leak")
	for _, tc := range []struct {
		replace bool
		want    string
	}{{replace: true, want: "absent"}, {replace: false, want: "present:leak"}} {
		proc, err := (ExecLauncher{}).Start(Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestEnvironmentHelper$"},
			Env: []string{"CONDUIT_PROCESS_ENV_HELPER=1"}, ReplaceEnv: tc.replace,
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		_ = proc.Stdin().Close()
		go io.Copy(io.Discard, proc.Stderr())
		output, err := io.ReadAll(proc.Stdout())
		if err != nil {
			t.Fatalf("read helper output: %v", err)
		}
		if result := proc.Wait(); result.ExitCode != 0 {
			t.Fatalf("helper exit %d: %v", result.ExitCode, result.Err)
		}
		if got := strings.TrimSpace(string(output)); got != tc.want {
			t.Fatalf("ReplaceEnv=%v: parent variable %q, want %q", tc.replace, got, tc.want)
		}
	}
}

func TestEnvironmentHelper(t *testing.T) {
	if os.Getenv("CONDUIT_PROCESS_ENV_HELPER") != "1" {
		return
	}
	if value, ok := os.LookupEnv("CONDUIT_PROCESS_PARENT_ONLY"); ok {
		fmt.Println("present:" + value)
	} else {
		fmt.Println("absent")
	}
	os.Exit(0)
}

// The root exits while its descendant keeps stdout open: RootExited fires
// although Wait still blocks on the pipe; KillTree then lets Wait complete.
func TestRootExitIsObservedWhileDescendantHoldsPipes(t *testing.T) {
	proc, err := (ExecLauncher{}).Start(Spec{
		Path: os.Args[0], Args: []string{"-test.run=TestOwnedProcessHelper", "--", "orphan"},
		Env: []string{"CONDUIT_PROCESS_HELPER=1"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	go io.Copy(io.Discard, proc.Stderr())
	reader := bufio.NewReader(proc.Stdout())
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse descendant PID %q: %v", line, err)
	}
	go io.Copy(io.Discard, reader)
	rootExited, rootResult := ObserveRootExit(proc)
	select {
	case <-rootExited:
	case <-time.After(5 * time.Second):
		_ = proc.KillTree()
		t.Fatal("root exit was not observed while a descendant held the pipes")
	}
	done := make(chan Result, 1)
	if result := rootResult(); result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("root result = %+v, want a clean exit", result)
	}
	go func() { done <- proc.Wait() }()
	select {
	case <-done:
		t.Fatal("Wait returned while a descendant still held stdout")
	case <-time.After(200 * time.Millisecond):
	}
	if err := proc.KillTree(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("KillTree after root exit: %v", err)
	}
	select {
	case result := <-done:
		if result.ExitCode != 0 {
			t.Fatalf("root result = %+v, want its own exit code 0", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not complete after the group kill")
	}
	if err := proc.Stdin().Close(); err != nil {
		t.Fatalf("stdin Close after Wait = %v, want the first close result", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(childPID, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("descendant %d survived the group kill", childPID)
}
