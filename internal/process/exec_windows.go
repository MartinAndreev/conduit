//go:build windows

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/MartinAndreev/conduit/internal/lockscope"
	"golang.org/x/sys/windows"
)

// Each owned child starts suspended, enters a kill-on-close Job Object, then
// resumes. No user code can spawn descendants before job assignment. Nested-job
// constraints are left to AssignProcessToJobObject: failure terminates the still
// suspended child and fails Start, never silently weakens ownership.
type ownedResources struct {
	mu  sync.Mutex
	job windows.Handle
}

func prepareOwnedProcess(cmd *exec.Cmd) (*ownedResources, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create owned process job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure owned process job: %w", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	return &ownedResources{job: job}, nil
}
func (o *ownedResources) activate(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open suspended process for ownership: %w", err)
	}
	defer windows.CloseHandle(handle)
	if err = windows.AssignProcessToJobObject(o.job, handle); err != nil {
		return fmt.Errorf("assign suspended process to owned job (nested job constraints may prevent ownership): %w", err)
	}
	// os/exec closes CreateProcess's initial thread handle. Enumerate the one
	// suspended initial thread and reopen it solely for ResumeThread.
	thread, err := initialThread(uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(thread)
	previous, err := windows.ResumeThread(thread)
	if err != nil {
		return fmt.Errorf("resume owned process: %w", err)
	}
	if previous != 1 {
		return fmt.Errorf("unexpected suspended process thread count: %d", previous)
	}
	return nil
}
func initialThread(pid uint32) (windows.Handle, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, fmt.Errorf("enumerate suspended process thread: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	var threadID uint32
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == pid {
			if threadID != 0 {
				return 0, errors.New("suspended process has unexpected multiple threads")
			}
			threadID = entry.ThreadID
		}
	}
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return 0, fmt.Errorf("inspect suspended process thread: %w", err)
	}
	if threadID == 0 {
		return 0, errors.New("suspended initial process thread not found")
	}
	handle, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, threadID)
	if err != nil {
		return 0, fmt.Errorf("open suspended process thread: %w", err)
	}
	return handle, nil
}
func (o *ownedResources) close() error {
	return lockscope.WithMutexLock(&o.mu, func() error {
		if o.job == 0 {
			return nil
		}
		err := windows.CloseHandle(o.job)
		if err == nil {
			o.job = 0
		}
		return err
	})
}
func (o *ownedResources) kill(_ int) error {
	return lockscope.WithMutexLock(&o.mu, func() error {
		if o.job == 0 {
			return os.ErrProcessDone
		}
		return windows.TerminateJobObject(o.job, 1)
	})
}
func interruptOwnedProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(os.Interrupt)
}
func ownedTreeKillSupported() bool          { return true }
func processSignal(*os.ProcessState) string { return "" }
