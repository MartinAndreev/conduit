//go:build aix || dragonfly || freebsd || netbsd || openbsd || solaris

package process

import (
	"errors"
	"os/exec"
)

// observeRootExit reaps the root at exit: these platforms have no verified way
// to observe the exit without reaping (Go's os uses wait6 WNOWAIT on the
// FreeBSD family, which is not implemented or tested here). execProcess then
// refuses group signals; see its documentation for the remaining window.
func observeRootExit(cmd *exec.Cmd) (Result, bool) { return waitRoot(cmd), false }

// RootRetentionSupported reports whether this platform keeps an exited root
// unreaped until Finalize, so a group kill after the root's exit can never
// reach a reused process group. It is false here: the root is reaped at exit
// and group signals are refused from then on, so cleanup after a root exit can
// never confirm descendant termination. Owners that need that guarantee (the
// startup probes) refuse to start children on these platforms.
func RootRetentionSupported() bool { return false }

// rootWaitable is unused here: the root is never held.
func rootWaitable(int) (bool, error) { return false, errors.ErrUnsupported }

// zombieOnlyGroup has no portable query here; a refused group signal stays an
// error.
func zombieOnlyGroup(int) bool { return false }
