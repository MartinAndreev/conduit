package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/childprocess"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine"
)

// Cross-process behavior is proven with child processes of this test binary.
// Each child waits at a start barrier, opens, reports, signals Reported and
// keeps its store open until the parent releases it.
const (
	childPathVariable         = "CONDUIT_TURSO_CHILD_PATH"
	childMultiprocessVariable = "CONDUIT_TURSO_CHILD_MULTIPROCESS"
	childAppendVariable       = "CONDUIT_TURSO_CHILD_APPEND"
)

// childReported is signalled after the child printed its result; an opened
// child still holds the database then.
const childReported = "reported"

type childRequest struct {
	multiprocess bool
	// appendEvent, when set, is appended after opening; the child then reports
	// every event ID it can read.
	appendEvent string
}

// childOutcome is what one child process reported.
type childOutcome struct {
	opened  bool
	locked  bool
	version string
	events  []string
	failure string
}

// TestChildProcessOpen is the child. It reports one CHILD-RESULT line and
// holds the database open until released.
func TestChildProcessOpen(t *testing.T) {
	path := os.Getenv(childPathVariable)
	if path == "" {
		t.Skip("child process helper")
	}
	if err := childprocess.AwaitBegin(); err != nil {
		t.Fatal(err)
	}
	// Deferred calls run in reverse: the store closes only after the release.
	var store *Store
	defer func() {
		if store != nil {
			store.Close()
		}
	}()
	defer func() {
		if err := childprocess.Signal(childReported); err != nil {
			t.Error(err)
		}
		if err := childprocess.AwaitRelease(); err != nil {
			t.Error(err)
		}
	}()
	ctx := context.Background()
	var err error
	store, err = OpenWithOptions(ctx, path, Options{MultiprocessWAL: os.Getenv(childMultiprocessVariable) == "1"})
	if errors.Is(err, ErrDatabaseLocked) {
		fmt.Printf("CHILD-RESULT locked %v\n", err)
		return
	}
	if err != nil {
		fmt.Printf("CHILD-RESULT failed %v\n", err)
		return
	}
	var version string
	if err := store.db.QueryRowContext(ctx, "SELECT value FROM conduit_metadata WHERE key = 'schema_version'").Scan(&version); err != nil {
		fmt.Printf("CHILD-RESULT failed read version: %v\n", err)
		return
	}
	var ids []string
	if id := os.Getenv(childAppendVariable); id != "" {
		if _, err := store.Events().Append(ctx, v1Event(id)); err != nil {
			fmt.Printf("CHILD-RESULT failed append: %v\n", err)
			return
		}
		ids, err = eventIDs(ctx, store)
		if err != nil {
			fmt.Printf("CHILD-RESULT failed replay: %v\n", err)
			return
		}
	}
	fmt.Printf("CHILD-RESULT opened version=%s events=%s\n", version, strings.Join(ids, ","))
}

func eventIDs(ctx context.Context, store *Store) ([]string, error) {
	replayed, err := store.Events().Replay(ctx, "run", 0, 100)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(replayed))
	for _, event := range replayed {
		ids = append(ids, string(event.ID))
	}
	return ids, nil
}

func startChild(t *testing.T, path string, request childRequest) *childprocess.Child {
	t.Helper()
	env := []string{childPathVariable + "=" + path, childAppendVariable + "=" + request.appendEvent}
	if request.multiprocess {
		env = append(env, childMultiprocessVariable+"=1")
	}
	return childprocess.Start(t, "TestChildProcessOpen", env...)
}

// runChild lets one child open alone and returns its outcome once it exited.
func runChild(t *testing.T, path string, request childRequest) childOutcome {
	t.Helper()
	child := startChild(t, path, request)
	child.Begin()
	return childResult(t, child)
}

// raceChildren starts two children through one start barrier and releases
// them only after both reported, so their opens overlap.
func raceChildren(t *testing.T, path string, request childRequest) []childOutcome {
	t.Helper()
	first, second := startChild(t, path, request), startChild(t, path, request)
	childprocess.BeginTogether(first, second)
	first.Await(childReported)
	second.Await(childReported)
	return []childOutcome{childResult(t, first), childResult(t, second)}
}

func childResult(t *testing.T, child *childprocess.Child) childOutcome {
	t.Helper()
	output := child.Finish()
	for _, line := range strings.Split(output, "\n") {
		result, ok := strings.CutPrefix(line, "CHILD-RESULT ")
		if !ok {
			continue
		}
		kind, detail, _ := strings.Cut(result, " ")
		outcome := childOutcome{opened: kind == "opened", locked: kind == "locked", failure: detail}
		if outcome.opened {
			outcome.failure = ""
			for _, field := range strings.Fields(detail) {
				if value, ok := strings.CutPrefix(field, "version="); ok {
					outcome.version = value
				}
				if value, ok := strings.CutPrefix(field, "events="); ok && value != "" {
					outcome.events = strings.Split(value, ",")
				}
			}
		}
		return outcome
	}
	t.Fatalf("child reported nothing:\n%s", output)
	return childOutcome{}
}

func requireMultiprocessPlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Turso ignores multiprocess_wal on Windows")
	}
}

func openRawWithOptions(t *testing.T, path string, options Options) *sql.DB {
	t.Helper()
	connector, err := engine.NewConnector(path, options, 5000)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenRefusesPathWithDSNParameters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db?experimental=multiprocess_wal")
	for _, options := range []Options{{}, {MultiprocessWAL: true}} {
		if _, err := OpenWithOptions(context.Background(), path, options); !errors.Is(err, engine.ErrPathHasQuery) {
			t.Fatalf("open %+v = %v", options, err)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused path was created: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatalf("directory after refusal = %v, %v", entries, err)
	}
}

// Default options: Turso allows one process per file. A running v1 process
// keeps the database; the second process gets ErrDatabaseLocked, the database
// stays v1 and the v1 process continues.
func TestCrossProcessOpenIsLockedWhileV1ProcessHoldsDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	v1 := createV1Database(t, path)
	outcome := runChild(t, path, childRequest{})
	if !outcome.locked {
		t.Fatalf("v2 process outcome = %+v", outcome)
	}
	if got := schemaVersion(t, v1); got != "1" {
		t.Fatalf("schema_version = %q", got)
	}
	if _, err := (eventRepository{db: v1, begin: v1.BeginTx}).Append(context.Background(), v1Event("v1-after-child")); err != nil {
		t.Fatalf("v1 append after the refused v2 open: %v", err)
	}
}

// Default options: of two processes opening a v1 file together, the one that
// gets the file migrates it; the other is locked out while the first holds it,
// so it sees neither v2 nor a busy error. With the single-process lock the
// second never reads v1, so this is not a race between two migrators.
func TestCrossProcessLosingMigratorIsLockedOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	outcomes := raceChildren(t, path, childRequest{})
	opened, locked := 0, 0
	for _, outcome := range outcomes {
		if outcome.opened {
			opened++
		}
		if outcome.locked {
			locked++
		}
	}
	if opened != 1 || locked != 1 {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	if got := schemaVersion(t, openRaw(t, path, 5000)); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
}

func TestCrossProcessMultiprocessWALSharesDatabase(t *testing.T) {
	requireMultiprocessPlatform(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shared.db")
	parent, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if _, err := parent.Events().Append(ctx, v1Event("parent")); err != nil {
		t.Fatal(err)
	}
	child := runChild(t, path, childRequest{multiprocess: true, appendEvent: "child"})
	if !child.opened || !slices.Equal(child.events, []string{"parent", "child"}) {
		t.Fatalf("child outcome = %+v", child)
	}
	ids, err := eventIDs(ctx, parent)
	if err != nil || !slices.Equal(ids, []string{"parent", "child"}) {
		t.Fatalf("parent sees %v, %v", ids, err)
	}
	if _, err := parent.Events().Append(ctx, v1Event("parent-after-child")); err != nil {
		t.Fatalf("parent append after the child's commit: %v", err)
	}
	if _, err := os.Stat(path + "-tshm"); err != nil {
		t.Fatalf("multiprocess sidecar: %v", err)
	}
}

// initializeSharedWAL opens and closes path once with the option on, which
// leaves Turso's <db>-tshm coordination file behind for later processes.
func initializeSharedWAL(t *testing.T, path string) {
	t.Helper()
	db := openRawWithOptions(t, path, Options{MultiprocessWAL: true})
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-tshm"); err != nil {
		t.Fatalf("coordination file after close: %v", err)
	}
}

func assertHealthyV2(t *testing.T, path string) {
	t.Helper()
	db := openRawWithOptions(t, path, Options{MultiprocessWAL: true})
	if got := schemaVersion(t, db); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
	assertCurrentSchema(t, db)
	var integrity string
	if err := db.QueryRowContext(context.Background(), "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q, %v", integrity, err)
	}
}

// With the option on and the coordination file already initialized, two
// processes that open a v1 file together both succeed and the database ends at
// v2. Whether the second read v1 before the first committed is not controlled:
// that would need a barrier inside Open. The re-read of the version under the
// write lock is proven in one process by
// TestMigratorThatLostTheRaceRereadsUnderWriteLock.
func TestCrossProcessMultiprocessWALMigratorsRace(t *testing.T) {
	requireMultiprocessPlatform(t)
	for round := range 3 {
		t.Run(strconv.Itoa(round), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v1.db")
			createV1Database(t, path).Close()
			initializeSharedWAL(t, path)
			outcomes := raceChildren(t, path, childRequest{multiprocess: true})
			for _, outcome := range outcomes {
				if !outcome.opened || outcome.version != "2" {
					t.Fatalf("outcomes = %+v", outcomes)
				}
			}
			assertHealthyV2(t, path)
		})
	}
}

// With the option on and no coordination file yet, two processes opening
// together race on creating <db>-tshm; with tursogo v0.8.3 the loser failed
// about half the rounds with ErrTursoCorrupt ("shared WAL coordination map
// magic mismatch" or "... smaller than the coordination header"). The bounded
// retry absorbs it: both processes open, the database ends at v2. The rounds
// are few because tursogo v0.8.3 rarely aborts a process in this race with a
// native panic (about 1 in 480 rounds) that no retry can catch.
func TestCrossProcessMultiprocessWALFirstOpenRaceIsRetried(t *testing.T) {
	requireMultiprocessPlatform(t)
	for round := range 4 {
		t.Run(strconv.Itoa(round), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v1.db")
			createV1Database(t, path).Close()
			if _, err := os.Stat(path + "-tshm"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("coordination file before the race: %v", err)
			}
			outcomes := raceChildren(t, path, childRequest{multiprocess: true})
			for _, outcome := range outcomes {
				if !outcome.opened || outcome.version != "2" {
					t.Fatalf("outcomes = %+v", outcomes)
				}
			}
			assertHealthyV2(t, path)
		})
	}
}

// Observed with tursogo v0.8.3, not guaranteed: mixing a flagged and an
// unflagged process is not refused, although Turso's documentation says it
// is. Both directions bypass the single-process lock, and in both the
// holder's open connection does not see the other process's committed row. This
// demonstrates why the option is read only from the shared configuration.
func TestCrossProcessMixedModeObservedBehavior(t *testing.T) {
	requireMultiprocessPlatform(t)
	ctx := context.Background()
	t.Run("flagged holder, unflagged writer", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mixed.db")
		holder, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Close()
		if _, err := holder.Events().Append(ctx, v1Event("holder")); err != nil {
			t.Fatal(err)
		}
		child := runChild(t, path, childRequest{appendEvent: "unflagged"})
		if !child.opened || !slices.Equal(child.events, []string{"holder", "unflagged"}) {
			t.Fatalf("unflagged child outcome = %+v", child)
		}
		ids, err := eventIDs(ctx, holder)
		if err != nil || !slices.Equal(ids, []string{"holder"}) {
			t.Fatalf("flagged holder sees %v, %v; observed v0.8.3 behavior was a lost view of the child's row", ids, err)
		}
	})
	t.Run("unflagged holder, flagged writer", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mixed.db")
		holder, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Close()
		if _, err := holder.Events().Append(ctx, v1Event("holder")); err != nil {
			t.Fatal(err)
		}
		child := runChild(t, path, childRequest{multiprocess: true, appendEvent: "flagged"})
		if !child.opened || !slices.Equal(child.events, []string{"holder", "flagged"}) {
			t.Fatalf("flagged child outcome = %+v", child)
		}
		ids, err := eventIDs(ctx, holder)
		if err != nil || !slices.Equal(ids, []string{"holder"}) {
			t.Fatalf("unflagged holder sees %v, %v; observed v0.8.3 behavior was a lost view of the child's row", ids, err)
		}
	})
}
