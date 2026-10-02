// Package lockscope owns the synchronous mutex scope used by harness adapters.
package lockscope

import "sync"

// WithMutexLock calls operation exactly once while holding mu. The operation is
// mandatory and synchronous: it must not defer its state work to a goroutine or
// acquire the same mutex. Unlock runs on return or panic; errors and panics pass
// through unchanged. Harness I/O and event publication belong outside this scope.
func WithMutexLock(mu *sync.Mutex, operation func() error) error {
	mu.Lock()
	defer mu.Unlock()
	return operation()
}
