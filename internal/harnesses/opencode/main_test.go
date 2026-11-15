package opencode

import (
	"fmt"
	"os"
	"testing"
	"time"

	processruntime "github.com/MartinAndreev/conduit/internal/process"
)

// TestMain fails a passing run that left a launched run client unfinalized:
// every run path must end in Finalize.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		deadline := time.Now().Add(5 * time.Second)
		for processruntime.UnfinalizedProcesses() != 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if leaked := processruntime.UnfinalizedProcesses(); leaked != 0 {
			fmt.Fprintf(os.Stderr, "%d launched run clients were not finalized\n", leaked)
			code = 1
		}
	}
	os.Exit(code)
}
