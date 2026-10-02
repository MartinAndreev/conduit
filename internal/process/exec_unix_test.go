//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	_ = child.Wait()
	os.Exit(0)
}
