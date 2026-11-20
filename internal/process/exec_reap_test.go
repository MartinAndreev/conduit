//go:build darwin || linux

package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// awaitStopped waits until pid is a zombie or gone. A killed orphan may stay
// a zombie under a slow or non-reaping PID 1, which still proves the kill.
func awaitStopped(t *testing.T, pid int, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for processRunning(t, pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s %d still runs", what, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// finalizeRoot finalizes proc and checks that its root PID was released.
func finalizeRoot(t *testing.T, proc Process) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Finalize(ctx, proc); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if err := syscall.Kill(proc.PID(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("root %d after Finalize: %v, want released (ESRCH)", proc.PID(), err)
	}
}

func assertSignalsRefused(t *testing.T, proc Process) {
	t.Helper()
	if err := proc.KillTree(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("KillTree after Finalize = %v, want os.ErrProcessDone", err)
	}
	if err := proc.Interrupt(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Interrupt after Finalize = %v, want os.ErrProcessDone", err)
	}
}

// assertZombieRoot checks that the exited root still holds its PID.
func assertZombieRoot(t *testing.T, proc Process) {
	t.Helper()
	if err := syscall.Kill(proc.PID(), 0); err != nil || processRunning(t, proc.PID()) {
		t.Fatalf("exited root %d is not held as a zombie (kill: %v)", proc.PID(), err)
	}
}

// The root exits while its descendant keeps stdout and stderr open. The root
// stays an unreaped zombie, which keeps its PID and so its process group
// number reserved: KillTree still reaches the descendant. Neither the exit
// nor Wait reaps the root; Finalize does, and later signals are refused
// without a syscall.
func TestRootStaysUnreapedUntilPipesEnd(t *testing.T) {
	proc, err := (ExecLauncher{}).Start(Spec{
		Path: os.Args[0], Args: []string{"-test.run=TestOwnedProcessHelper", "--", "orphan"},
		Env: []string{"CONDUIT_PROCESS_HELPER=1"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ownProcess(t, proc)
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
	rootExited, _ := ObserveRootExit(proc)
	select {
	case <-rootExited:
	case <-time.After(5 * time.Second):
		t.Fatal("root exit was not observed while a descendant held the pipes")
	}
	assertZombieRoot(t, proc)
	done := make(chan Result, 1)
	go func() { done <- proc.Wait() }()
	select {
	case <-done:
		t.Fatal("Wait returned while a descendant still held stdout")
	case <-time.After(200 * time.Millisecond):
	}
	assertZombieRoot(t, proc)
	if err := proc.KillTree(); err != nil {
		t.Fatalf("KillTree with a zombie root and a live descendant: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not complete after the group kill")
	}
	awaitStopped(t, childPID, "descendant")
	assertZombieRoot(t, proc)
	finalizeRoot(t, proc)
	assertSignalsRefused(t, proc)
}

// RootResult is observed before the reap, yet reports what exec.Cmd.Wait
// reports for the same exit, and Wait reports the same. A group that only
// holds the zombie root is reported as done (Darwin answers it with EPERM,
// Linux delivers to it).
func TestRootResultMatchesExecExit(t *testing.T) {
	for _, tc := range []struct {
		script string
		code   int
		signal string
	}{
		{script: "exit 0", code: 0},
		{script: "exit 3", code: 3},
		{script: "kill -KILL $$", code: -1, signal: syscall.SIGKILL.String()},
	} {
		t.Run(tc.script, func(t *testing.T) {
			reference := exec.Command("/bin/sh", "-c", tc.script).Run()
			proc, err := (ExecLauncher{}).Start(Spec{Path: "/bin/sh", Args: []string{"-c", tc.script}})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			ownProcess(t, proc)
			rootExited, rootResult := ObserveRootExit(proc)
			select {
			case <-rootExited:
			case <-time.After(5 * time.Second):
				_ = proc.KillTree()
				t.Fatal("root exit was not observed")
			}
			if err := proc.KillTree(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Fatalf("KillTree with only the zombie root: %v", err)
			}
			observed := rootResult()
			go io.Copy(io.Discard, proc.Stderr())
			_, _ = io.Copy(io.Discard, proc.Stdout())
			waited := proc.Wait()
			if observed.ExitCode != tc.code || observed.Signal != tc.signal {
				t.Fatalf("RootResult = %+v, want code %d signal %q", observed, tc.code, tc.signal)
			}
			if waited.ExitCode != observed.ExitCode || waited.Signal != observed.Signal || !waited.ExitedAt.Equal(observed.ExitedAt) {
				t.Fatalf("Wait = %+v, RootResult = %+v: the same exit differs", waited, observed)
			}
			if reference == nil {
				if observed.Err != nil || waited.Err != nil {
					t.Fatalf("clean exit errors: RootResult %v, Wait %v", observed.Err, waited.Err)
				}
				return
			}
			if observed.Err == nil || observed.Err.Error() != reference.Error() || waited.Err == nil || waited.Err.Error() != reference.Error() {
				t.Fatalf("RootResult error %v, Wait error %v, want the text of exec's %v", observed.Err, waited.Err, reference)
			}
		})
	}
}

// A descendant that redirected its stdio does not keep the pipes open, so
// Wait returns after the root exit. The zombie root stays unreaped, which
// keeps the group number reserved: a KillTree after Wait still kills the
// descendant; Finalize then reaps the root and signalling ends.
func TestHeldRootLetsKillTreeReachRedirectedDescendant(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := fmt.Sprintf("/bin/sleep 30 </dev/null >/dev/null 2>&1 &\nprintf '%%s' \"$!\" > '%s'\nexit 0\n", pidFile)
	proc, err := (ExecLauncher{}).Start(Spec{Path: "/bin/sh", Args: []string{"-c", script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ownProcess(t, proc)
	go io.Copy(io.Discard, proc.Stderr())
	go io.Copy(io.Discard, proc.Stdout())
	done := make(chan Result, 1)
	go func() { done <- proc.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after the root exit")
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	assertZombieRoot(t, proc)
	if err := proc.KillTree(); err != nil {
		t.Fatalf("KillTree after Wait with a live descendant: %v", err)
	}
	awaitStopped(t, childPID, "descendant")
	assertZombieRoot(t, proc)
	finalizeRoot(t, proc)
	assertSignalsRefused(t, proc)
}

// The root exits while a descendant that left the process group holds
// stderr, so Wait stays blocked and KillTree cannot reach the descendant.
// Finalize does not depend on either: it reaps the exited root, and from then
// on no signal is sent. The descendant blocks on stdin, which the test
// closes to end it; nothing depends on a sleep.
func TestFinalizeReapsRootWhileEscapedDescendantHoldsStderr(t *testing.T) {
	proc, err := (ExecLauncher{}).Start(Spec{
		Path: os.Args[0], Args: []string{"-test.run=^TestEscapedDescendantHelper$"},
		Env: []string{"CONDUIT_ESCAPED_HELPER=root"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ownProcess(t, proc)
	stderr := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, proc.Stderr()); close(stderr) }()
	output, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("parse descendant PID %q: %v", output, err)
	}
	rootExited, _ := ObserveRootExit(proc)
	select {
	case <-rootExited:
	case <-time.After(5 * time.Second):
		t.Fatal("root exit was not observed")
	}
	if err := proc.KillTree(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("KillTree: %v", err)
	}
	if !processRunning(t, childPID) {
		t.Fatalf("escaped descendant %d did not survive the group kill", childPID)
	}
	finalizeRoot(t, proc)
	assertSignalsRefused(t, proc)
	select {
	case <-stderr:
		t.Fatal("stderr ended although the escaped descendant holds it")
	default:
	}
	if !processRunning(t, childPID) {
		t.Fatalf("escaped descendant %d was signalled", childPID)
	}
	// Closing stdin ends the descendant, and with it stderr and Wait.
	if err := proc.Stdin().Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	select {
	case <-stderr:
	case <-time.After(5 * time.Second):
		t.Fatal("stderr did not end after the descendant's stdin closed")
	}
	proc.Wait()
}

// TestEscapedDescendantHelper: as root, starts itself as a descendant in its
// own process group that keeps the root's stdin and stderr (not stdout),
// prints its PID and exits. As descendant, it reads stdin until EOF.
func TestEscapedDescendantHelper(t *testing.T) {
	switch os.Getenv("CONDUIT_ESCAPED_HELPER") {
	case "root":
		child := exec.Command(os.Args[0], "-test.run=^TestEscapedDescendantHelper$")
		child.Env = append(os.Environ(), "CONDUIT_ESCAPED_HELPER=descendant")
		child.Stdin, child.Stderr = os.Stdin, os.Stderr
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println(child.Process.Pid)
		os.Exit(0)
	case "descendant":
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
}

// The fallback used when the exit cannot be awaited (waitid or kqueue
// failed) polls the root's state and reads its status without reaping it.
func TestPollRootExitObservesWithoutReap(t *testing.T) {
	for _, tc := range []struct {
		script string
		code   int
		signal string
	}{
		{script: "sleep 0.1; exit 4", code: 4},
		{script: "sleep 0.1; kill -KILL $$", code: -1, signal: syscall.SIGKILL.String()},
	} {
		t.Run(tc.script, func(t *testing.T) {
			cmd := exec.Command("/bin/sh", "-c", tc.script)
			if err := cmd.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { _ = cmd.Wait() })
			result := pollRootExit(cmd.Process.Pid)
			if result.ExitCode != tc.code || result.Signal != tc.signal || result.Err == nil {
				t.Fatalf("polled result = %+v, want code %d signal %q", result, tc.code, tc.signal)
			}
			if err := syscall.Kill(cmd.Process.Pid, 0); err != nil || processRunning(t, cmd.Process.Pid) {
				t.Fatalf("polled root %d is not held as a zombie (kill: %v)", cmd.Process.Pid, err)
			}
		})
	}
}
