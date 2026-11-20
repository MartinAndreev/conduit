package process

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// RootRetentionSupported is true: the exited root stays unreaped until
// Finalize (see exec_unix_other.go for the platforms without it).
func RootRetentionSupported() bool { return true }

// observeRootExit observes the exit without reaping, so the root is held.
func observeRootExit(cmd *exec.Cmd) (Result, bool) { return awaitRootExit(cmd.Process.Pid), true }

// awaitRootExit blocks in waitid(WEXITED|WNOWAIT), as os does before its own
// reap: the root stays a zombie, so its PID and process group number stay
// reserved until exec.Cmd.Wait reaps it. If waitid fails (for example ENOSYS
// in a restricted sandbox), the root's /proc state is polled instead; the
// root is never reaped here. Without waitid the reap can never be authorized:
// Finalize commits and ends with ErrReleaseUnavailable (unreaped, counted).
// Without both waitid and /proc the exit is never observed and Wait does not
// return.
func awaitRootExit(pid int) Result {
	var info siginfoChild
	for {
		err := waitidNoReap(pid, &info, 0)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return pollRootExit(pid)
		}
		return exitedResult(info.waitStatus())
	}
}

func waitidNoReap(pid int, info *siginfoChild, options int) error {
	return unix.Waitid(unix.P_PID, pid, (*unix.Siginfo)(unsafe.Pointer(info)), unix.WEXITED|unix.WNOWAIT|options, nil)
}

// pollRootExit waits, with backoff, until the whole root process exited (see
// zombieResult). Read failures (such as EMFILE) are retried.
func pollRootExit(pid int) Result {
	for delay := exitPollMin; ; delay = nextPollDelay(delay) {
		if result, exited := zombieResult(pid); exited {
			return result
		}
		time.Sleep(delay)
	}
}

// errExitStatusUnknown is the root error when neither waitid nor /proc report
// the status of a root known to have exited.
var errExitStatusUnknown = errors.New("root exited with an unknown status")

// zombieResult reports the status of a root whose whole process exited: from
// waitid when it works again, otherwise from the leader's exit_code (stat
// field 52, Linux 3.5 and later), which holds the wait status, once every
// thread is a zombie. That /proc evidence only publishes the exit (a hint
// for RootExited); it never authorizes the reap, which needs rootWaitable.
func zombieResult(pid int) (Result, bool) {
	var info siginfoChild
	switch err := waitidNoReap(pid, &info, unix.WNOHANG); {
	case err == nil && info.signo != 0:
		return exitedResult(info.waitStatus()), true
	case err == nil || !threadGroupExited(pid):
		return Result{}, false
	}
	fields, err := procStat(strconv.Itoa(pid))
	if err != nil {
		return Result{}, false
	}
	if len(fields) >= 52-2 {
		if status, err := strconv.Atoi(fields[52-3]); err == nil {
			return exitedResult(syscall.WaitStatus(status)), true
		}
	}
	return Result{ExitCode: -1, Err: errExitStatusUnknown, ExitedAt: time.Now()}, true
}

// rootWaitable reports, without blocking or reaping, that the whole process
// exited, as only waitid(WNOHANG|WNOWAIT) can establish it: a /proc snapshot
// cannot (a thread can be created and another exit between its reads). If
// waitid is unusable the error is returned and the root stays unreaped.
func rootWaitable(pid int) (bool, error) {
	var info siginfoChild
	for {
		err := waitidNoReap(pid, &info, unix.WNOHANG)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return false, err
		}
		return info.signo != 0, nil
	}
}

// threadGroupExited reports that every thread of pid is a zombie (or dead);
// any failure to establish that is reported as not exited. It is a snapshot,
// not proof: it only serves the exit observation fallback.
func threadGroupExited(pid int) bool {
	tasks, err := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/task")
	if err != nil || len(tasks) == 0 {
		return false
	}
	for _, task := range tasks {
		fields, err := procStat(strconv.Itoa(pid) + "/task/" + task.Name())
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ESRCH) {
			continue // the thread was released during the scan
		}
		if err != nil || (fields[0] != "Z" && fields[0] != "X") {
			return false
		}
	}
	return true
}

// procStat returns the fields of /proc/<path>/stat after "pid (comm)": state
// first (field 3), so field n is at index n-3. comm may contain spaces and
// parentheses, so parsing starts after the last ')'.
func procStat(path string) ([]string, error) {
	stat, err := os.ReadFile("/proc/" + path + "/stat")
	if err != nil {
		return nil, err
	}
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return nil, fmt.Errorf("malformed /proc/%s/stat", path)
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 3 {
		return nil, fmt.Errorf("malformed /proc/%s/stat", path)
	}
	return fields, nil
}

// zombieOnlyGroup is never needed on Linux: it delivers to a group that only
// holds zombies instead of refusing it with EPERM.
func zombieOnlyGroup(int) bool { return false }

// siginfoChild is the SIGCHLD layout of the 128-byte siginfo_t filled by
// waitid (as os's internal SiginfoChild): the union starts after three int32
// fields, padded to 8 bytes on 64-bit hosts. MIPS swaps si_errno and si_code.
type siginfoChild struct {
	signo     int32
	errnoCode [2]int32
	_         [unsafe.Sizeof(uintptr(0)) / 8]int32
	_         int32  // si_pid
	_         uint32 // si_uid
	status    int32
	_         [128 - (6+unsafe.Sizeof(uintptr(0))/8)*4]byte
}

// Values of si_code for SIGCHLD; CLD_EXITED is the remaining case.
const (
	cldKilled = 2
	cldDumped = 3
)

// waitStatus converts the exit siginfo into the wait status that wait4 (and
// so exec.Cmd.Wait) reports for the same exit.
func (s *siginfoChild) waitStatus() syscall.WaitStatus {
	code := s.errnoCode[1]
	if strings.HasPrefix(runtime.GOARCH, "mips") {
		code = s.errnoCode[0]
	}
	// WEXITED reports only exited, killed and dumped children.
	switch code {
	case cldDumped:
		return syscall.WaitStatus(s.status) | 0x80
	case cldKilled:
		return syscall.WaitStatus(s.status)
	}
	return syscall.WaitStatus(s.status << 8)
}
