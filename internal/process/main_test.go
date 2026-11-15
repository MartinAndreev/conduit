package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// testMainHooks run before the tests; a helper process started from this test
// binary does its work, and exits, inside one.
var testMainHooks []func()

// TestMain fails a passing run that left a launched process unfinalized.
func TestMain(m *testing.M) {
	for _, hook := range testMainHooks {
		hook()
	}
	code := m.Run()
	if code == 0 {
		if leaked := awaitFinalized(5 * time.Second); leaked != 0 {
			fmt.Fprintf(os.Stderr, "%d launched processes were not finalized\n", leaked)
			code = 1
		}
	}
	os.Exit(code)
}

// awaitFinalized waits for asynchronous owners (CleanupOwner) to finalize and
// returns the number of processes still unfinalized.
func awaitFinalized(limit time.Duration) int {
	deadline := time.Now().Add(limit)
	for UnfinalizedProcesses() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return UnfinalizedProcesses()
}

// ownProcess ends a test's process through its owned lifecycle only, also
// after a failed assertion: KillTree (refused once finalized), closing the
// pipes, Wait, then Finalize, each bounded and with its failure reported.
func ownProcess(t *testing.T, proc Process) {
	t.Helper()
	t.Cleanup(func() {
		if err := proc.KillTree(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("cleanup KillTree: %v", err)
		}
		_ = proc.Stdin().Close()
		_ = proc.Stdout().Close()
		_ = proc.Stderr().Close()
		done := make(chan struct{})
		go func() { proc.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("cleanup Wait did not return")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := Finalize(ctx, proc); err != nil {
			t.Errorf("cleanup Finalize: %v", err)
		}
	})
}
