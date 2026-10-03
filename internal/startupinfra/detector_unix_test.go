//go:build darwin || linux

package startupinfra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/configuration"
)

func TestExitedProbeCleansRedirectedDescendant(t *testing.T) {
	for _, exitCode := range []int{0, 3} {
		t.Run(fmt.Sprint(exitCode), func(t *testing.T) {
			directory := t.TempDir()
			executable := filepath.Join(directory, "codex")
			pidFile := filepath.Join(directory, "descendant.pid")
			script := fmt.Sprintf("#!/bin/sh\n/bin/sleep 30 </dev/null >/dev/null 2>&1 &\nprintf '%%s' \"$!\" > '%s'\nprintf 'codex-cli 1.2.3\\n'\nexit %d\n", pidFile, exitCode)
			if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			// Always clean the fixture, including assertion failures against broken code.
			defer func() {
				if data, err := os.ReadFile(pidFile); err == nil {
					if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			}()
			observation, err := (Detector{}).Discover(context.Background(), configuration.Codex, executable)
			want := configuration.Found
			if exitCode != 0 {
				want = configuration.ProbeFailed
			}
			if err != nil || observation.Kind != want {
				t.Fatal(observation, err)
			}
			data, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("descendant %d survived root exit %d", pid, exitCode)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
