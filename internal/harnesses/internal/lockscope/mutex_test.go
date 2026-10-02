package lockscope

import (
	"errors"
	"sync"
	"testing"
)

func TestWithMutexLockRunsOnceAndPreservesError(t *testing.T) {
	sentinel := errors.New("callback failure")
	for _, tc := range []struct {
		name   string
		result error
	}{{"success", nil}, {"error", sentinel}} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			err := WithMutexLock(&mu, func() error {
				calls++
				if mu.TryLock() {
					mu.Unlock()
					t.Fatal("callback ran without lock")
				}
				return tc.result
			})
			if calls != 1 || err != tc.result {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
			if !mu.TryLock() {
				t.Fatal("return left mutex locked")
			}
			mu.Unlock()
		})
	}
}

func TestWithMutexLockPropagatesPanicAndUnlocks(t *testing.T) {
	var mu sync.Mutex
	sentinel := &struct{ message string }{"callback panic"}
	calls := 0
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = WithMutexLock(&mu, func() error { calls++; panic(sentinel) })
	}()
	if recovered != sentinel || calls != 1 {
		t.Fatalf("panic=%v calls=%d", recovered, calls)
	}
	if !mu.TryLock() {
		t.Fatal("panic left mutex locked")
	}
	mu.Unlock()
}
