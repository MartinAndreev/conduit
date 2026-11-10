package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/childprocess"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine"
	tursogo "turso.tech/database/tursogo"
)

const (
	holderPathVariable         = "CONDUIT_CATALOG_HOLDER_PATH"
	holderMultiprocessVariable = "CONDUIT_CATALOG_HOLDER_MULTIPROCESS"
	readerPathVariable         = "CONDUIT_CATALOG_READER_PATH"
)

// holderOpened is signalled once the holder has the catalog open.
const holderOpened = "opened"

// TestChildProcessHoldsCatalog is the child: it opens the catalog, signals
// that and keeps the database open until the parent releases it.
func TestChildProcessHoldsCatalog(t *testing.T) {
	path := os.Getenv(holderPathVariable)
	if path == "" {
		t.Skip("child process helper")
	}
	db, conn, err := connectCatalog(context.Background(), path, Options{MultiprocessWAL: os.Getenv(holderMultiprocessVariable) == "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if err := childprocess.Signal(holderOpened); err != nil {
		t.Fatal(err)
	}
	if err := childprocess.AwaitRelease(); err != nil {
		t.Fatal(err)
	}
}

// holdCatalog starts a child process holding path and returns once it has the
// database open; the returned function releases the child and waits for it.
func holdCatalog(t *testing.T, path string, multiprocess bool) func() {
	t.Helper()
	env := []string{holderPathVariable + "=" + path}
	if multiprocess {
		env = append(env, holderMultiprocessVariable+"=1")
	}
	holder := childprocess.Start(t, "TestChildProcessHoldsCatalog", env...)
	holder.Await(holderOpened)
	return func() { holder.Finish() }
}

func TestCatalogHeldByAnotherProcessIsLockedNotCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	seed(t, path)
	wait := holdCatalog(t, path, false)
	defer wait()
	_, err := (Store{Path: path}).Read(context.Background())
	if !errors.Is(err, ErrDatabaseLocked) || errors.Is(err, app.ErrCorruptCatalog) {
		t.Fatalf("read while held = %v", err)
	}
	if _, err := (Store{Path: path}).Insert(context.Background(), project(2)); !errors.Is(err, ErrDatabaseLocked) {
		t.Fatalf("insert while held = %v", err)
	}
}

func TestCatalogMultiprocessWALSharesCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Turso ignores multiprocess_wal on Windows")
	}
	path := filepath.Join(t.TempDir(), "catalog.db")
	seed(t, path)
	wait := holdCatalog(t, path, true)
	defer wait()
	store := Store{Path: path, Options: Options{MultiprocessWAL: true}}
	if result, err := store.Insert(context.Background(), project(2)); err != nil || !result.Persisted {
		t.Fatalf("insert while shared = %#v, %v", result, err)
	}
	snapshot, err := store.Read(context.Background())
	if err != nil || len(snapshot.Projects) != 2 {
		t.Fatalf("read while shared = %#v, %v", snapshot, err)
	}
}

func TestCatalogRefusesPathWithDSNParameters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db?experimental=multiprocess_wal")
	store := Store{Path: path}
	if _, err := store.Read(context.Background()); !errors.Is(err, engine.ErrPathHasQuery) {
		t.Fatalf("read = %v", err)
	}
	if _, err := store.Insert(context.Background(), project(1)); !errors.Is(err, engine.ErrPathHasQuery) {
		t.Fatalf("insert = %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatalf("directory after refusal = %v, %v", entries, err)
	}
}

// TestChildProcessReadsCatalog is the child: behind the start barrier it
// reads the catalog with multi-process WAL and reports one CHILD-RESULT line.
func TestChildProcessReadsCatalog(t *testing.T) {
	path := os.Getenv(readerPathVariable)
	if path == "" {
		t.Skip("child process helper")
	}
	if err := childprocess.AwaitBegin(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (Store{Path: path, Options: Options{MultiprocessWAL: true}}).Read(context.Background())
	if err != nil {
		fmt.Printf("CHILD-RESULT failed %v\n", err)
		return
	}
	fmt.Printf("CHILD-RESULT read %d\n", len(snapshot.Projects))
}

func readerResult(t *testing.T, reader *childprocess.Child) string {
	t.Helper()
	output := reader.Finish()
	for _, line := range strings.Split(output, "\n") {
		if result, ok := strings.CutPrefix(line, "CHILD-RESULT "); ok {
			return result
		}
	}
	t.Fatalf("reader reported nothing:\n%s", output)
	return ""
}

// Two flagged readers of an existing catalog without <catalog>-tshm race on
// creating it, as the session store does; the bounded retry lets both read.
// Few rounds: tursogo v0.8.3 rarely aborts a process in this race.
func TestCatalogMultiprocessWALFirstOpenRaceIsRetried(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Turso ignores multiprocess_wal on Windows")
	}
	for round := range 4 {
		t.Run(strconv.Itoa(round), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			seed(t, path)
			if _, err := os.Stat(path + "-tshm"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("coordination file before the race: %v", err)
			}
			env := readerPathVariable + "=" + path
			first := childprocess.Start(t, "TestChildProcessReadsCatalog", env)
			second := childprocess.Start(t, "TestChildProcessReadsCatalog", env)
			childprocess.BeginTogether(first, second)
			if results := []string{readerResult(t, first), readerResult(t, second)}; results[0] != "read 1" || results[1] != "read 1" {
				t.Fatalf("readers = %q", results)
			}
		})
	}
}

func TestConnectionFailureClassification(t *testing.T) {
	race := fmt.Errorf("%w: shared WAL coordination map magic mismatch", tursogo.ErrTursoCorrupt)
	for _, tc := range []struct {
		name string
		err  error
		want error
		not  error
	}{
		{"unsettled coordination file", fmt.Errorf("%w: 5 attempts made: %w", engine.ErrSharedMemoryInit, race), ErrSharedMemoryInit, app.ErrCorruptCatalog},
		{"locked", fmt.Errorf("%w: File is locked by another process", tursogo.ErrTursoGeneric), ErrDatabaseLocked, app.ErrCorruptCatalog},
		{"race without retry", race, app.ErrCorruptCatalog, ErrSharedMemoryInit},
	} {
		got := connectionFailure(tc.err)
		if !errors.Is(got, tc.want) || errors.Is(got, tc.not) || !errors.Is(got, tc.err) {
			t.Fatalf("%s: %v", tc.name, got)
		}
	}
}
