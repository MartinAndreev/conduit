package process

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// processRunning reports whether pid exists and is not a zombie (or dead); a
// failed query other than the process being gone fails the test.
func processRunning(t *testing.T, pid int) bool {
	t.Helper()
	fields, err := procStat(strconv.Itoa(pid))
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ESRCH) {
		return false
	}
	if err != nil {
		t.Fatalf("query process %d: %v", pid, err)
	}
	return fields[0] != "Z" && fields[0] != "X"
}

// The leader-exit helper needs its main goroutine, which runs TestMain's
// hooks, on the main thread (the thread-group leader); LockOSThread in init
// guarantees that.
func init() {
	runtime.LockOSThread()
	testMainHooks = append(testMainHooks, func() {
		if os.Getenv("CONDUIT_LEADER_EXIT_HELPER") == "1" {
			leaderExitHelper()
		}
	})
}

// leaderExitHelper ends only the main thread, so the leader becomes a zombie
// while the process keeps running; another thread exits the process with
// status 7 once stdin is closed.
func leaderExitHelper() {
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(7)
	}()
	time.Sleep(50 * time.Millisecond) // let the reader block in its own thread
	_, _, _ = syscall.Syscall(syscall.SYS_EXIT, 0, 0, 0)
}

// A zombie thread-group leader with a running thread is not a process exit:
// RootExited stays open and neither the waitability check nor the /proc
// fallback treats the leader's Z state as an exit.
func TestZombieLeaderWithLiveThreadIsNotExit(t *testing.T) {
	proc, err := (ExecLauncher{}).Start(Spec{Path: os.Args[0], Env: []string{"CONDUIT_LEADER_EXIT_HELPER=1"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ownProcess(t, proc)
	go io.Copy(io.Discard, proc.Stdout())
	go io.Copy(io.Discard, proc.Stderr())
	pid := strconv.Itoa(proc.PID())
	deadline := time.Now().Add(5 * time.Second)
	for {
		fields, err := procStat(pid)
		if err != nil {
			t.Fatalf("read leader state: %v", err)
		}
		if fields[0] == "Z" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("leader state %q, want Z", fields[0])
		}
		time.Sleep(10 * time.Millisecond)
	}
	if threadGroupExited(proc.PID()) {
		t.Fatal("thread group reported exited while a thread runs")
	}
	if waitable, err := rootWaitable(proc.PID()); waitable || err != nil {
		t.Fatalf("root reported waitable (%v, %v) while a thread runs", waitable, err)
	}
	if _, exited := zombieResult(proc.PID()); exited {
		t.Fatal("fallback reported an exit while a thread runs")
	}
	select {
	case <-proc.(RootExitObserver).RootExited():
		t.Fatal("RootExited closed while a thread runs")
	default:
	}
	if err := proc.Stdin().Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	select {
	case <-proc.(RootExitObserver).RootExited():
	case <-time.After(5 * time.Second):
		t.Fatal("process exit was not observed")
	}
	if result := proc.(RootExitObserver).RootResult(); result.ExitCode != 7 {
		t.Fatalf("RootResult = %+v, want exit 7", result)
	}
	if waitable, err := rootWaitable(proc.PID()); !threadGroupExited(proc.PID()) || !waitable || err != nil {
		t.Fatalf("exited process is not reported waitable (%v, %v)", waitable, err)
	}
	if result, exited := zombieResult(proc.PID()); !exited || result.ExitCode != 7 {
		t.Fatalf("fallback result = %+v (exited %v), want exit 7", result, exited)
	}
}
