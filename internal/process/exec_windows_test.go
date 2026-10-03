//go:build windows

package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOwnedWindowsHelper(t *testing.T) {
	mode := os.Getenv("CONDUIT_WINDOWS_JOB_TEST")
	if mode == "" {
		return
	}
	switch mode {
	case "tree":
		executable, _ := os.Executable()
		cmd := exec.Command(executable, "-test.run=^TestOwnedWindowsHelper$")
		cmd.Env = append(os.Environ(), "CONDUIT_WINDOWS_JOB_TEST=sleep")
		if err := cmd.Start(); err != nil {
			os.Exit(4)
		}
		fmt.Println(cmd.Process.Pid)
		time.Sleep(30 * time.Second)
	case "sleep":
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}
func TestWindowsOwnedJobKillsDescendants(t *testing.T) {
	executable, _ := os.Executable()
	p, err := (ExecLauncher{}).Start(Spec{Path: executable, Args: []string{"-test.run=^TestOwnedWindowsHelper$"}, Env: []string{"CONDUIT_WINDOWS_JOB_TEST=tree"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.KillTree()
	_ = p.Stdin().Close()
	line := make(chan string, 1)
	go func() {
		var b [1]byte
		var value strings.Builder
		for {
			n, err := p.Stdout().Read(b[:])
			if n > 0 {
				value.Write(b[:n])
				if b[0] == '\n' {
					line <- value.String()
					break
				}
			}
			if err != nil {
				line <- ""
				break
			}
		}
		_, _ = io.Copy(io.Discard, p.Stdout())
	}()
	go io.Copy(io.Discard, p.Stderr())
	var descendant int
	select {
	case value := <-line:
		descendant, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			t.Fatal(value, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not run after suspended assignment")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(descendant))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if !p.TreeKillSupported() {
		t.Fatal("job ownership unsupported")
	}
	if err = p.KillTree(); err != nil {
		t.Fatal(err)
	}
	done := make(chan Result, 1)
	go func() { done <- p.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owned process not reaped")
	}
	event, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatal("descendant survived", event, err)
	}
	if err = p.KillTree(); err != os.ErrProcessDone {
		t.Fatal("closed job retained", err)
	}
}
func TestWindowsJobReleasedWhenLaunchFails(t *testing.T) {
	cmd := exec.Command("conduit-definitely-missing.exe")
	owned, err := prepareOwnedProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err == nil {
		t.Fatal("unexpected launch")
	}
	if err = owned.close(); err != nil {
		t.Fatal(err)
	}
	if owned.job != 0 {
		t.Fatal("job handle retained")
	}
	if err = owned.close(); err != nil {
		t.Fatal("close is not idempotent", err)
	}
}
