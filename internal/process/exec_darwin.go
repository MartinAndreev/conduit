package process

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// RootRetentionSupported is true: the exited root stays unreaped until
// Finalize (see exec_unix_other.go for the platforms without it).
func RootRetentionSupported() bool { return true }

// observeRootExit observes the exit without reaping, so the root is held.
func observeRootExit(cmd *exec.Cmd) (Result, bool) { return awaitRootExit(cmd.Process.Pid), true }

// Darwin's waitid also returns for stopped children (golang.org/issue/19314),
// so the exit is observed with kqueue: EVFILT_PROC NOTE_EXIT|NOTE_EXITSTATUS
// delivers the wait status without reaping. Registration fails with ESRCH once
// the root is already a zombie, and kqueue itself can fail (for example
// EMFILE); the root's state is then polled with sysctl, which needs no file
// descriptor and does not reap either.
func awaitRootExit(pid int) Result {
	kq, err := unix.Kqueue()
	if err != nil {
		return pollRootExit(pid)
	}
	defer unix.Close(kq)
	var change unix.Kevent_t
	unix.SetKevent(&change, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	change.Fflags = unix.NOTE_EXIT | unix.NOTE_EXITSTATUS
	err = retryEINTR(func() error {
		_, err := unix.Kevent(kq, []unix.Kevent_t{change}, nil, nil)
		return err
	})
	if err != nil {
		return pollRootExit(pid)
	}
	events := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(kq, nil, events, nil)
		if errors.Is(err, syscall.EINTR) || (err == nil && n == 0) {
			continue
		}
		if err != nil {
			return pollRootExit(pid)
		}
		return exitedResult(syscall.WaitStatus(events[0].Data))
	}
}

// pollRootExit waits, with backoff, until sysctl reports the root a zombie.
// Query failures are retried.
func pollRootExit(pid int) Result {
	for delay := exitPollMin; ; delay = nextPollDelay(delay) {
		if result, exited := zombieResult(pid); exited {
			return result
		}
		time.Sleep(delay)
	}
}

// darwinZombie is SZOMB from <sys/proc.h>.
const darwinZombie = 5

// zombieResult reads the wait status (P_xstat) of the unreaped root once it
// is a zombie.
func zombieResult(pid int) (Result, bool) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info.Proc.P_stat != darwinZombie {
		return Result{}, false
	}
	return exitedResult(syscall.WaitStatus(info.Proc.P_xstat)), true
}

// rootWaitable reports that the root is a zombie: on Darwin a process becomes
// SZOMB only after all its threads exited.
func rootWaitable(pid int) (bool, error) {
	return zombieWaitability(func() (*unix.KinfoProc, error) {
		return unix.SysctlKinfoProc("kern.proc.pid", pid)
	}, time.Sleep)
}

// Darwin process states from <sys/proc.h> that precede SZOMB.
const (
	darwinIdle     = 1 // SIDL
	darwinRunnable = 2 // SRUN
	darwinSleeping = 3 // SSLEEP
	darwinStopped  = 4 // SSTOP
)

// transientQueryAttempts bounds the retries of one transient sysctl failure.
const transientQueryAttempts = 5

// zombieWaitability classifies the kern.proc.pid answer for a root whose exit
// was observed. SZOMB is waitable. A state that precedes it (the exit is
// still completing) is a negative answer the worker retries. A transient
// query failure (EINTR, EAGAIN, ENOMEM) is retried here a bounded number of
// times. Any other failure (EPERM, EACCES, EIO for a missing entry, ...), an
// unknown state, or transient failures beyond the bound end finalization:
// the error is returned with the query cause.
func zombieWaitability(query func() (*unix.KinfoProc, error), sleep func(time.Duration)) (bool, error) {
	delay := exitPollMin
	for attempt := 1; ; attempt++ {
		info, err := query()
		if err == nil {
			switch info.Proc.P_stat {
			case darwinZombie:
				return true, nil
			case darwinIdle, darwinRunnable, darwinSleeping, darwinStopped:
				return false, nil
			default:
				return false, fmt.Errorf("root process state %d is neither live nor a zombie", info.Proc.P_stat)
			}
		}
		transient := errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENOMEM)
		if !transient || attempt == transientQueryAttempts {
			return false, fmt.Errorf("query root process state: %w", err)
		}
		sleep(delay)
		delay = nextPollDelay(delay)
	}
}

// zombieOnlyGroup reports whether every member of the process group is a
// zombie; Darwin answers kill(-pgid) with EPERM for such a group. A failed
// query answers false, keeping the EPERM.
func zombieOnlyGroup(pgid int) bool {
	members, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return false
	}
	for _, member := range members {
		if member.Proc.P_stat != darwinZombie {
			return false
		}
	}
	return true
}

func retryEINTR(call func() error) error {
	for {
		if err := call(); !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
