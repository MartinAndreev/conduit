package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine/rules"
	tursogo "turso.tech/database/tursogo"
)

func TestDSN(t *testing.T) {
	for _, tc := range []struct {
		path    string
		options Options
		want    string
		err     error
	}{
		{"/tmp/state.db", Options{}, "/tmp/state.db", nil},
		{"/tmp/state.db", Options{MultiprocessWAL: true}, "/tmp/state.db?experimental=multiprocess_wal", nil},
		{"/tmp/odd?experimental=x.db", Options{}, "", ErrPathHasQuery},
		{"/tmp/state.db?_busy_timeout=1", Options{MultiprocessWAL: true}, "", ErrPathHasQuery},
	} {
		got, err := DSN(tc.path, tc.options)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Fatalf("DSN(%q, %+v) = %q, %v", tc.path, tc.options, got, err)
		}
	}
}

func TestIsLockedByAnotherProcess(t *testing.T) {
	locked := fmt.Errorf("%w: Locking error: Failed locking file '/x.db'. File is locked by another process", tursogo.ErrTursoGeneric)
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"engine lock refusal", locked, true},
		{"wrapped", fmt.Errorf("open: %w", locked), true},
		{"busy", fmt.Errorf("%w: database is locked", tursogo.ErrTursoBusy), false},
		{"message without engine sentinel", errors.New("File is locked by another process"), false},
		{"other generic", fmt.Errorf("%w: no such table", tursogo.ErrTursoGeneric), false},
		{"nil", nil, false},
	} {
		if got := IsLockedByAnotherProcess(tc.err); got != tc.want {
			t.Fatalf("%s: got %v", tc.name, got)
		}
	}
}

func sharedMemoryRaceError(detail string) error {
	return fmt.Errorf("%w: %s", tursogo.ErrTursoCorrupt, detail)
}

func TestIsSharedMemoryInitRace(t *testing.T) {
	magic := sharedMemoryRaceError("shared WAL coordination map magic mismatch")
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"magic mismatch", magic, true},
		{"header not written yet", sharedMemoryRaceError("shared WAL coordination file is smaller than the coordination header: got 0, minimum 4096"), true},
		{"wrapped", fmt.Errorf("open: %w", magic), true},
		{"joined", errors.Join(errors.New("open catalog connection"), magic), true},
		{"other coordination corruption", sharedMemoryRaceError("shared WAL coordination map slot count mismatch: got 1"), false},
		{"database corruption", sharedMemoryRaceError("malformed database schema"), false},
		{"message without corrupt sentinel", fmt.Errorf("%w: shared WAL coordination map magic mismatch", tursogo.ErrTursoGeneric), false},
		{"plain message", errors.New("shared WAL coordination map magic mismatch"), false},
		{"lock refusal", fmt.Errorf("%w: Failed locking shared WAL coordination file. File is locked by another process", tursogo.ErrTursoGeneric), false},
		{"nil", nil, false},
	} {
		if got := IsSharedMemoryInitRace(tc.err); got != tc.want {
			t.Fatalf("%s: got %v", tc.name, got)
		}
	}
}

// countingAttempt fails with the given errors in order, then succeeds.
func countingAttempt(failures ...error) (func() error, *int) {
	calls := 0
	return func() error {
		calls++
		if calls <= len(failures) {
			return failures[calls-1]
		}
		return nil
	}, &calls
}

func TestRetrySharedMemoryInit(t *testing.T) {
	race := sharedMemoryRaceError("shared WAL coordination map magic mismatch")
	flagged := Options{MultiprocessWAL: true}
	ctx := context.Background()

	t.Run("settles", func(t *testing.T) {
		attempt, calls := countingAttempt(race, race)
		if err := RetrySharedMemoryInit(ctx, flagged, attempt); err != nil || *calls != 3 {
			t.Fatalf("err = %v after %d attempts", err, *calls)
		}
	})
	t.Run("exhausted", func(t *testing.T) {
		attempt, calls := countingAttempt(race, race, race, race, race, race)
		err := RetrySharedMemoryInit(ctx, flagged, attempt)
		if !errors.Is(err, ErrSharedMemoryInit) || !errors.Is(err, tursogo.ErrTursoCorrupt) || *calls != rules.SharedMemoryInitMaxAttempts {
			t.Fatalf("err = %v after %d attempts", err, *calls)
		}
	})
	t.Run("flag off is not retried", func(t *testing.T) {
		attempt, calls := countingAttempt(race)
		err := RetrySharedMemoryInit(ctx, Options{}, attempt)
		if err != race || *calls != 1 {
			t.Fatalf("err = %v after %d attempts", err, *calls)
		}
	})
	t.Run("other failure is not retried", func(t *testing.T) {
		corrupt := sharedMemoryRaceError("malformed database schema")
		attempt, calls := countingAttempt(corrupt)
		err := RetrySharedMemoryInit(ctx, flagged, attempt)
		if err != corrupt || *calls != 1 {
			t.Fatalf("err = %v after %d attempts", err, *calls)
		}
	})
	t.Run("cancelled wait", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		attempt, calls := countingAttempt(race, race)
		err := RetrySharedMemoryInit(cancelled, flagged, attempt)
		if !errors.Is(err, ErrSharedMemoryInit) || !errors.Is(err, context.Canceled) || !errors.Is(err, tursogo.ErrTursoCorrupt) || *calls != 1 {
			t.Fatalf("err = %v after %d attempts", err, *calls)
		}
	})
}
