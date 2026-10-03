//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type ownedResources struct{}

func prepareOwnedProcess(cmd *exec.Cmd) (*ownedResources, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &ownedResources{}, nil
}
func (*ownedResources) activate(int) error { return nil }
func (*ownedResources) close() error       { return nil }
func (*ownedResources) kill(pid int) error { return killOwnedProcessTree(pid) }

func interruptOwnedProcess(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGINT)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func killOwnedProcessTree(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
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
