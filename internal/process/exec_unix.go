//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

type ownedResources struct{}

func prepareOwnedProcess(cmd *exec.Cmd) (*ownedResources, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &ownedResources{}, nil
}
func (*ownedResources) activate(int) error { return nil }
func (*ownedResources) close() error       { return nil }

// newRootOps addresses the root's process group by number (PGID == root PID),
// so execProcess gates its signals on the reap.
func newRootOps(cmd *exec.Cmd, _ *ownedResources) rootOps {
	pid := cmd.Process.Pid
	return rootOps{
		awaitExit:       func() (Result, bool) { return observeRootExit(cmd) },
		waitable:        func() (bool, error) { return rootWaitable(pid) },
		reap:            func() error { return reapExited(cmd) },
		interrupt:       func() error { return signalOwnedGroup(pid, syscall.SIGINT) },
		killTree:        func() error { return signalOwnedGroup(pid, syscall.SIGKILL) },
		signalsByNumber: true,
	}
}

// signalOwnedGroup signals every member of the root's process group. ESRCH
// means the group is empty. Darwin refuses a group that only holds zombies
// (such as the held root) with EPERM, which is reported as os.ErrProcessDone
// when the group is confirmed to hold zombies only.
func signalOwnedGroup(pgid int, signal syscall.Signal) error {
	err := syscall.Kill(-pgid, signal)
	if errors.Is(err, syscall.ESRCH) || (errors.Is(err, syscall.EPERM) && zombieOnlyGroup(pgid)) {
		return os.ErrProcessDone
	}
	return err
}

func ownedTreeKillSupported() bool { return true }

func processSignal(state *os.ProcessState) string {
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}

// exitedResult converts a status observed without reaping into the Result
// that exec.Cmd.Wait reports for the same exit.
func exitedResult(status syscall.WaitStatus) Result {
	result := Result{ExitCode: status.ExitStatus(), ExitedAt: time.Now()}
	if status.Signaled() {
		result.Signal = status.Signal().String()
	}
	if !status.Exited() || status.ExitStatus() != 0 {
		result.Err = &rootExitError{status: status}
	}
	return result
}

// rootExitError is RootResult's error for an unsuccessful exit observed
// before the reap. Its text matches the *exec.ExitError that Wait reports.
type rootExitError struct{ status syscall.WaitStatus }

func (e *rootExitError) Error() string {
	var text string
	switch {
	case e.status.Exited():
		text = "exit status " + strconv.Itoa(e.status.ExitStatus())
	case e.status.Signaled():
		text = "signal: " + e.status.Signal().String()
	default:
		text = "unexpected wait status " + strconv.Itoa(int(e.status))
	}
	if e.status.CoreDump() {
		text += " (core dumped)"
	}
	return text
}
