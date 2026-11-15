package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// startZombie starts a root in its own group that exits at once and waits
// until it is a zombie, which kqueue can no longer attach to.
func startZombie(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for !zombieOnlyGroup(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("root did not become a zombie")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cmd
}

// A root that is already a zombie when the exit observation starts is read
// through sysctl, still without reaping it.
func TestAwaitRootExitReadsZombieStatus(t *testing.T) {
	cmd := startZombie(t, "exit 5")
	result := awaitRootExit(cmd.Process.Pid)
	if result.ExitCode != 5 || result.Err == nil || result.Err.Error() != "exit status 5" {
		t.Fatalf("zombie result = %+v, want exit status 5", result)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("observation reaped the zombie: %v", err)
	}
}

// Darwin refuses a group that only holds the zombie root with EPERM; that is
// an exited group, not a failed kill.
func TestZombieOnlyGroupIsReportedDone(t *testing.T) {
	cmd := startZombie(t, "exit 0")
	if err := signalOwnedGroup(cmd.Process.Pid, syscall.SIGKILL); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("group kill with only the zombie root = %v, want os.ErrProcessDone", err)
	}
}

// processRunning reports whether pid exists and is not a zombie; a failed
// query fails the test instead of passing as stopped.
func processRunning(t *testing.T, pid int) bool {
	t.Helper()
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		t.Fatalf("query process %d: %v", pid, err)
	}
	return len(procs) == 1 && procs[0].Proc.P_stat != darwinZombie
}

// The Darwin waitability adapter classifies the sysctl answer: SZOMB is
// waitable, a live state is retried by the worker, transient query failures
// are retried here a bounded number of times, and every other failure or
// unknown state ends finalization with the query cause.
func TestZombieWaitabilityClassifiesQueryAnswers(t *testing.T) {
	state := func(stat int8) func() (*unix.KinfoProc, error) {
		return func() (*unix.KinfoProc, error) {
			var info unix.KinfoProc
			info.Proc.P_stat = stat
			return &info, nil
		}
	}
	failing := func(errs ...error) (func() (*unix.KinfoProc, error), *int) {
		calls := 0
		return func() (*unix.KinfoProc, error) {
			calls++
			if calls <= len(errs) {
				return nil, errs[calls-1]
			}
			var info unix.KinfoProc
			info.Proc.P_stat = darwinZombie
			return &info, nil
		}, &calls
	}
	noSleep := func(time.Duration) {}
	if waitable, err := zombieWaitability(state(darwinZombie), noSleep); !waitable || err != nil {
		t.Fatalf("zombie = %v, %v; want waitable", waitable, err)
	}
	if waitable, err := zombieWaitability(state(darwinRunnable), noSleep); waitable || err != nil {
		t.Fatalf("exiting root = %v, %v; want a retryable negative answer", waitable, err)
	}
	if _, err := zombieWaitability(state(9), noSleep); err == nil {
		t.Fatal("unknown state did not end finalization")
	}
	query, calls := failing(syscall.EINTR, syscall.EAGAIN)
	if waitable, err := zombieWaitability(query, noSleep); !waitable || err != nil || *calls != 3 {
		t.Fatalf("transient failures = %v, %v after %d queries; want waitable after retries", waitable, err, *calls)
	}
	query, calls = failing(syscall.EPERM)
	if _, err := zombieWaitability(query, noSleep); !errors.Is(err, syscall.EPERM) || *calls != 1 {
		t.Fatalf("permanent failure = %v after %d queries; want EPERM at once", err, *calls)
	}
	endless := make([]error, 2*transientQueryAttempts)
	for i := range endless {
		endless[i] = syscall.ENOMEM
	}
	query, calls = failing(endless...)
	if _, err := zombieWaitability(query, noSleep); !errors.Is(err, syscall.ENOMEM) || *calls != transientQueryAttempts {
		t.Fatalf("persistent transient failure = %v after %d queries; want ENOMEM after %d", err, *calls, transientQueryAttempts)
	}
}
