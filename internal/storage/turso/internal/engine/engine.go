// Package engine owns how Conduit's Turso stores address the embedded engine:
// the open options, the DSN built from a plain file path, the classification
// of the engine's lock refusal and of the coordination file creation race, and
// the bounded retry of that race. Both the session store and the
// project catalog use the same driver, so this technical policy has one owner.
package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine/rules"
	tursogo "turso.tech/database/tursogo"
)

// Options are the engine settings a store is opened with. The zero value is
// the default: one process per database file.
type Options struct {
	// MultiprocessWAL enables Turso's experimental multi-process WAL
	// (?experimental=multiprocess_wal, sidecar <db>-tshm). Turso honours it only
	// on 64-bit Unix with a local filesystem and ignores it on Windows.
	MultiprocessWAL bool
}

// ErrPathHasQuery refuses a path containing '?'. tursogo splits its DSN at the
// first '?' without unescaping, so such a path cannot be passed safely: its
// tail would be parsed as engine parameters.
var ErrPathHasQuery = errors.New("database path must not contain '?'")

const multiprocessWALParameter = "?experimental=multiprocess_wal"

// DSN returns the tursogo DSN for a plain file path. The path itself is still
// what callers stat and publish; only the engine sees the parameters.
func DSN(path string, options Options) (string, error) {
	if strings.Contains(path, "?") {
		return "", ErrPathHasQuery
	}
	if options.MultiprocessWAL {
		return path + multiprocessWALParameter, nil
	}
	return path, nil
}

// NewConnector builds a tursogo connector for path with options.
func NewConnector(path string, options Options, busyTimeoutMilliseconds int) (*tursogo.TursoConnector, error) {
	dsn, err := DSN(path, options)
	if err != nil {
		return nil, err
	}
	return tursogo.NewConnector(dsn, tursogo.WithBusyTimeout(busyTimeoutMilliseconds))
}

// lockedByAnotherProcess is the engine message for a file another process
// holds open without shared access. tursogo exposes no dedicated status or
// sentinel for it (it arrives as ErrTursoGeneric), so the message is matched.
const lockedByAnotherProcess = "File is locked by another process"

// IsLockedByAnotherProcess reports whether err is Turso's refusal to open a
// database file that another process holds.
func IsLockedByAnotherProcess(err error) bool {
	return err != nil && errors.Is(err, tursogo.ErrTursoGeneric) && strings.Contains(err.Error(), lockedByAnotherProcess)
}

// sharedMemoryInitRace are the engine messages, observed with tursogo v0.8.3,
// of an open that finds <db>-tshm created by another process but not yet
// initialized. They arrive as ErrTursoCorrupt, yet they describe the
// coordination file, not the database. A process that opens alone
// reinitializes an empty, zeroed or short coordination file, so with these
// messages another process is initializing it at the same moment.
var sharedMemoryInitRace = []string{
	"shared WAL coordination map magic mismatch",
	"shared WAL coordination file is smaller than the coordination header",
}

// IsSharedMemoryInitRace reports whether err is Turso's refusal to open while
// another process is creating the multi-process coordination file.
func IsSharedMemoryInitRace(err error) bool {
	if err == nil || !errors.Is(err, tursogo.ErrTursoCorrupt) {
		return false
	}
	message := err.Error()
	return slices.ContainsFunc(sharedMemoryInitRace, func(race string) bool { return strings.Contains(message, race) })
}

// ErrSharedMemoryInit reports that the coordination file creation race did
// not settle within the retry bound, or that waiting for it was cancelled.
var ErrSharedMemoryInit = errors.New("shared WAL coordination file creation race did not settle")

// RetrySharedMemoryInit runs attempt, the store's connector, pool and first
// connection, and repeats it while rules.DecideSharedMemoryInitRetry says so.
// A failed attempt must close everything it opened before returning. Other
// failures are returned unchanged; exhaustion and cancellation of a wait are
// wrapped in ErrSharedMemoryInit together with the last attempt's error.
func RetrySharedMemoryInit(ctx context.Context, options Options, attempt func() error) error {
	for attempts := 1; ; attempts++ {
		err := attempt()
		if err == nil {
			return nil
		}
		decision := rules.DecideSharedMemoryInitRetry(rules.SharedMemoryInitFacts{
			MultiprocessWAL: options.MultiprocessWAL,
			InitRace:        IsSharedMemoryInitRace(err),
			Attempts:        attempts,
		})
		switch decision.Kind {
		case rules.SharedMemoryInitRetry:
		case rules.SharedMemoryInitExhausted:
			return fmt.Errorf("%w: %s: %w", ErrSharedMemoryInit, decision.Reason, err)
		default:
			return err
		}
		if waitErr := wait(ctx, decision.Delay); waitErr != nil {
			return fmt.Errorf("%w: retry wait: %w", ErrSharedMemoryInit, errors.Join(waitErr, err))
		}
	}
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
