package turso

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/storage/turso/rules"
	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
	tursogo "turso.tech/database/tursogo"
)

var migrationTime = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// openRaw opens a database the way a v1 process did: no schema handling and no
// foreign key pragma.
func openRaw(t *testing.T, path string, busyTimeout int) *sql.DB {
	t.Helper()
	connector, err := tursogo.NewConnector(path, tursogo.WithBusyTimeout(busyTimeout))
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// createV1Database writes a v1 database with one session and one event and
// returns its still open handle, standing in for a running v1 process.
func createV1Database(t *testing.T, path string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db := openRaw(t, path, 5000)
	for _, statement := range schema.Version1Statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"schema_version": "1", "application_id": schema.ApplicationID} {
		if _, err := db.ExecContext(ctx, "INSERT INTO conduit_metadata (key, value) VALUES (?, ?)", key, value); err != nil {
			t.Fatal(err)
		}
	}
	record := sessions.Record{ID: "v1-session", ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: 1, AdapterSessionID: "adapter", State: "open", CreatedAt: migrationTime, UpdatedAt: migrationTime}
	if _, err := (sessionRepository{db: db}).Save(ctx, record, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := (eventRepository{db: db, begin: db.BeginTx}).Append(ctx, v1Event("v1-event-1")); err != nil {
		t.Fatal(err)
	}
	return db
}

func v1Event(id string) events.Event {
	return events.Event{ID: events.EventID(id), RunID: "run", Kind: "session.opened", PayloadVersion: 1, Payload: json.RawMessage(`{}`), Provenance: "conduit", CausationID: "command", ObservedAt: migrationTime}
}

func schemaVersion(t *testing.T, db database) string {
	t.Helper()
	var version string
	if err := db.QueryRowContext(context.Background(), "SELECT value FROM conduit_metadata WHERE key = 'schema_version'").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func tableDefinitions(t *testing.T, db database, tables ...string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, name := range tables {
		var definition string
		if err := db.QueryRowContext(context.Background(), "SELECT sql FROM sqlite_schema WHERE name = ?", name).Scan(&definition); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		result[name] = definition
	}
	return result
}

// assertCurrentSchema requires every v1 and v2 object with its exact definition.
func assertCurrentSchema(t *testing.T, db database) {
	t.Helper()
	if err := validateSchemaObjects(context.Background(), db, schema.Statements); err != nil {
		t.Fatal(err)
	}
}

func TestFreshDatabaseOpensAtCurrentSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fresh.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := schemaVersion(t, store.db); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
	assertCurrentSchema(t, store.db)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen current schema: %v", err)
	}
	store.Close()
}

func TestMigratesV1DatabaseWithDataIntact(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	v1 := createV1Database(t, path)
	before := tableDefinitions(t, v1, "conduit_metadata", "sessions", "event_journal", "event_journal_replay")
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := schemaVersion(t, store.db); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
	assertCurrentSchema(t, store.db)
	after := tableDefinitions(t, store.db, "conduit_metadata", "sessions", "event_journal", "event_journal_replay")
	for name, definition := range before {
		if after[name] != definition {
			t.Fatalf("migration changed %s:\n%s\n%s", name, definition, after[name])
		}
	}
	session, err := store.Sessions().Get(ctx, "v1-session")
	if err != nil || session.Version != 1 || !session.CreatedAt.Equal(migrationTime) {
		t.Fatalf("session = %#v, %v", session, err)
	}
	replayed, err := store.Events().Replay(ctx, "run", 0, 10)
	if err != nil || len(replayed) != 1 || replayed[0].ID != "v1-event-1" {
		t.Fatalf("events = %#v, %v", replayed, err)
	}
	if _, err := store.Events().Append(ctx, v1Event("v2-event-2")); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesUnsupportedSchemaVersionWithoutMutation(t *testing.T) {
	for _, version := range []string{"3", "0", "x"} {
		t.Run(version, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE conduit_metadata SET value = ? WHERE key = 'schema_version'", version); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Open(ctx, path); !errors.Is(err, ErrIncompatibleDatabase) {
				t.Fatalf("open error = %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("refusal mutated the database")
			}
		})
	}
}

// A v1 build compares schema_version with "1" through DecideSchemaMarker and
// maps a mismatch to ErrIncompatibleDatabase; the migrated marker triggers it.
func TestMigratedMarkerIsRefusedByV1Check(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	decision := rules.DecideSchemaMarker("schema_version", schemaVersion(t, store.db), "1")
	if decision.Kind != rules.SchemaMarkerMismatch {
		t.Fatalf("v1 check = %#v", decision)
	}
}

// Within one process Turso shares the engine between connectors, so an open
// v1 connection behaves like a v1 process on a multi-process SQLite file.
func TestOpenV1ConnectionKeepsWorkingAcrossMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	v1 := createV1Database(t, path)
	prepared, err := v1.PrepareContext(ctx, "SELECT count(*) FROM sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()

	// The v1 connection holds a write transaction when the migrator arrives;
	// the migrator waits for the write lock instead of failing.
	tx, err := v1.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (eventRepository{db: tx}).Append(ctx, v1Event("v1-during-migration")); err != nil {
		t.Fatal(err)
	}
	opened := make(chan error, 1)
	var store *Store
	go func() {
		var openErr error
		store, openErr = Open(ctx, path)
		opened <- openErr
	}()
	time.Sleep(200 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-opened; err != nil {
		t.Fatalf("migration while v1 held the write lock: %v", err)
	}
	defer store.Close()

	sessionsV1, eventsV1 := sessionRepository{db: v1}, eventRepository{db: v1, begin: v1.BeginTx}
	record, err := sessionsV1.Get(ctx, "v1-session")
	if err != nil {
		t.Fatal(err)
	}
	record.State, record.UpdatedAt = "idle", migrationTime.Add(time.Minute)
	if _, err := sessionsV1.Save(ctx, record, 1); err != nil {
		t.Fatalf("v1 session update after migration: %v", err)
	}
	if _, err := eventsV1.Append(ctx, v1Event("v1-after-migration")); err != nil {
		t.Fatalf("v1 append after migration: %v", err)
	}
	var count int
	if err := prepared.QueryRowContext(ctx).Scan(&count); err != nil || count != 1 {
		t.Fatalf("v1 prepared statement after migration: count=%d err=%v", count, err)
	}
	replayed, err := store.Events().Replay(ctx, "run", 0, 10)
	if err != nil || len(replayed) != 3 {
		t.Fatalf("events seen by v2 = %#v, %v", replayed, err)
	}
	got, err := store.Sessions().Get(ctx, "v1-session")
	if err != nil || got.Version != 2 || got.State != "idle" {
		t.Fatalf("session seen by v2 = %#v, %v", got, err)
	}
	// The migrated store must not hold a stale read snapshot from validation.
	if _, err := store.Events().Append(ctx, v1Event("v2-after-v1-write")); err != nil {
		t.Fatalf("v2 append after a v1 commit: %v", err)
	}
}

// The loser read v1 before the winner committed; under the write lock it reads
// v2 and changes nothing, so its statements never run twice.
func TestMigratorThatLostTheRaceRereadsUnderWriteLock(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	loser := sql.OpenDB(foreignKeyConnector{Connector: mustConnector(t, path, 5000)})
	defer loser.Close()
	decision, err := readSchemaMigrationDecision(ctx, loser)
	if err != nil || decision.Kind != rules.SchemaMigrationMigrateV1 {
		t.Fatalf("loser decision = %#v, %v", decision, err)
	}
	winner, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer winner.Close()
	if err := migrateSchema(ctx, loser); err != nil {
		t.Fatalf("loser migration: %v", err)
	}
	if got := schemaVersion(t, loser); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
}

func TestConcurrentMigratorsInOneProcess(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	const migrators = 8
	start := make(chan struct{})
	results := make(chan error, migrators)
	var wg sync.WaitGroup
	for range migrators {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			store, err := Open(ctx, path)
			if err == nil {
				err = store.Close()
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("migrator: %v", err)
		}
	}
	db := openRaw(t, path, 5000)
	if got := schemaVersion(t, db); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
	assertCurrentSchema(t, db)
}

// A busy error surfaces from each bounded attempt; once the lock is released
// the next migration succeeds.
func TestMigrationReportsBusyAfterBoundedAttempts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	holder := createV1Database(t, path)
	conn, err := holder.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	const busyTimeout = 50
	migrator := sql.OpenDB(foreignKeyConnector{Connector: mustConnector(t, path, busyTimeout)})
	defer migrator.Close()
	started := time.Now()
	err = migrateSchema(ctx, migrator)
	if !errors.Is(err, tursogo.ErrTursoBusy) {
		t.Fatalf("migration under a held lock = %v", err)
	}
	if elapsed := time.Since(started); elapsed < rules.MigrationMaxAttempts*busyTimeout*time.Millisecond {
		t.Fatalf("busy reported after %v, before %d attempts", elapsed, rules.MigrationMaxAttempts)
	}
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if err := migrateSchema(ctx, migrator); err != nil {
		t.Fatalf("migration after release: %v", err)
	}
	if got := schemaVersion(t, migrator); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
}

func mustConnector(t *testing.T, path string, busyTimeout int) *tursogo.TursoConnector {
	t.Helper()
	connector, err := tursogo.NewConnector(path, tursogo.WithBusyTimeout(busyTimeout))
	if err != nil {
		t.Fatal(err)
	}
	return connector
}
