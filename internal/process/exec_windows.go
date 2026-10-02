//go:build windows

package process

import (
	"os"
	"os/exec"
)

func configureOwnedProcess(_ *exec.Cmd) {}

func interruptOwnedProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(os.Interrupt)
}

func killOwnedProcessTree(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

// The standard library process handle does not provide Job Object descendant
// ownership. Callers must not infer tree quiescence from root termination.
func ownedTreeKillSupported() bool { return false }

func processSignal(*os.ProcessState) string { return "" }
