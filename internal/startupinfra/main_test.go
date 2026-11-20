package startupinfra

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/process"
)

// TestMain fails a passing run that left a launched probe unfinalized: every
// probe path must end in its cleanup owner's Finalize.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		deadline := time.Now().Add(5 * time.Second)
		for process.UnfinalizedProcesses() != 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if leaked := process.UnfinalizedProcesses(); leaked != 0 {
			fmt.Fprintf(os.Stderr, "%d launched probe processes were not finalized\n", leaked)
			code = 1
		}
	}
	os.Exit(code)
}
