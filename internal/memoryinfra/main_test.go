package memoryinfra

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/process"
)

// TestMain isolates the git the reader starts from the developer's global
// configuration: the reader keeps HOME and XDG_CONFIG_HOME, so they point at
// an empty directory here. It also fails a passing run that left a launched
// process unfinalized: every git path must end in finalization.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "memoryinfra-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", home)
	code := m.Run()
	if code == 0 {
		deadline := time.Now().Add(5 * time.Second)
		for process.UnfinalizedProcesses() != 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if leaked := process.UnfinalizedProcesses(); leaked != 0 {
			fmt.Fprintf(os.Stderr, "%d launched processes were not finalized\n", leaked)
			code = 1
		}
	}
	os.RemoveAll(home)
	os.Exit(code)
}
