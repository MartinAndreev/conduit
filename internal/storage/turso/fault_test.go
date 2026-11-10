package turso

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
	tursogo "turso.tech/database/tursogo"
)

// faultConnector injects failures into statements of real Turso connections:
// a statement starting with an injected prefix fails with its error instead of
// running, the given number of times or, with times < 0, always. It records
// statements and counts opened and closed driver connections.
type faultConnector struct {
	driver.Connector
	mu         sync.Mutex
	faults     map[string]*fault
	statements []string
	connects   atomic.Int32
	closes     atomic.Int32
}

func (c *faultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	c.connects.Add(1)
	return &faultConn{tursoConn: conn.(tursoConn), connector: c}, nil
}

type fault struct {
	err   error
	times int
}

func (c *faultConnector) inject(prefix string, err error, times int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.faults[prefix] = &fault{err: err, times: times}
}

func (c *faultConnector) fault(query string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, query)
	for prefix, injected := range c.faults {
		if !strings.HasPrefix(strings.TrimSpace(query), prefix) || injected.times == 0 {
			continue
		}
		if injected.times > 0 {
			injected.times--
		}
		return injected.err
	}
	return nil
}

func (c *faultConnector) count(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(slices.DeleteFunc(slices.Clone(c.statements), func(query string) bool { return !strings.HasPrefix(strings.TrimSpace(query), prefix) }))
}

type tursoConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ExecerContext
	driver.QueryerContext
}

type faultConn struct {
	tursoConn
	connector *faultConnector
}

func (c *faultConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.connector.fault(query); err != nil {
		return nil, err
	}
	return c.tursoConn.ExecContext(ctx, query, args)
}

func (c *faultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.connector.fault(query); err != nil {
		return nil, err
	}
	return c.tursoConn.QueryContext(ctx, query, args)
}

func (c *faultConn) Close() error {
	c.connector.closes.Add(1)
	return c.tursoConn.Close()
}

// openFaulty opens path through a fault connector the way Open configures its
// pool: one connection, foreign keys on.
func openFaulty(t *testing.T, path string, busyTimeout int) (*sql.DB, *faultConnector) {
	t.Helper()
	connector := &faultConnector{Connector: mustConnector(t, path, busyTimeout), faults: make(map[string]*fault)}
	db := sql.OpenDB(foreignKeyConnector{Connector: connector})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	return db, connector
}

func busyFault() error { return fmt.Errorf("%w: injected", tursogo.ErrTursoBusy) }

func fileContent(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// A failed ROLLBACK leaves the transaction state unknown: the busy attempt is
// not repeated, both failures are returned, and the driver connection is closed
// instead of being reused, which ends the engine transaction.
func TestBusyMigrationWithFailedRollbackIsNotRetried(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	db, connector := openFaulty(t, path, 5000)
	rollbackFailure := errors.New("injected rollback failure")
	connector.inject("CREATE TABLE memory_records", busyFault(), -1)
	connector.inject("ROLLBACK", rollbackFailure, -1)

	err := migrateSchema(ctx, db)
	if !errors.Is(err, tursogo.ErrTursoBusy) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("migration = %v", err)
	}
	if got := connector.count("BEGIN IMMEDIATE"); got != 1 {
		t.Fatalf("attempts = %d", got)
	}
	if got := connector.closes.Load(); got != 1 {
		t.Fatalf("driver connections closed = %d", got)
	}
	if got := schemaVersion(t, db); got != "1" {
		t.Fatalf("schema_version = %q", got)
	}
	if got := connector.connects.Load(); got != 2 {
		t.Fatalf("driver connections opened = %d; the uncertain one was reused", got)
	}
	if err := validateSchemaObjects(ctx, db, schema.Version1Statements); err != nil {
		t.Fatal(err)
	}
	if err := validateSchemaObjects(ctx, db, schema.Version2Statements[:1]); !errors.Is(err, ErrIncompatibleDatabase) {
		t.Fatalf("memory_records after the discarded attempt: %v", err)
	}
	// Closing the discarded connection released its write lock.
	connector.inject("CREATE TABLE memory_records", nil, 0)
	connector.inject("ROLLBACK", nil, 0)
	if err := migrateSchema(ctx, db); err != nil {
		t.Fatalf("migration after the discarded attempt: %v", err)
	}
	assertCurrentSchema(t, db)
}

// A busy attempt that rolled back is repeated; the next attempt migrates.
func TestBusyMigrationWithRollbackIsRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	db, connector := openFaulty(t, path, 5000)
	connector.inject("CREATE TABLE memory_records", busyFault(), 1)
	if err := migrateSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if got := connector.count("BEGIN IMMEDIATE"); got != 2 {
		t.Fatalf("attempts = %d", got)
	}
	if got := connector.closes.Load(); got != 0 {
		t.Fatalf("driver connections closed = %d", got)
	}
	assertCurrentSchema(t, db)
}

// A statement failing in the middle of the migration rolls back everything:
// on reopen the database is still v1 with its data, and then migrates.
func TestFailedMigrationLeavesV1Intact(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	db, connector := openFaulty(t, path, 5000)
	statementFailure := fmt.Errorf("%w: injected", tursogo.ErrTursoGeneric)
	connector.inject("CREATE TABLE memory_reviews", statementFailure, -1)
	if err := migrateSchema(ctx, db); !errors.Is(err, statementFailure) {
		t.Fatalf("migration = %v", err)
	}
	if got := connector.count("CREATE TABLE memory_records"); got != 1 {
		t.Fatalf("earlier v2 statements ran %d times", got)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	raw := openRaw(t, path, 5000)
	if got := schemaVersion(t, raw); got != "1" {
		t.Fatalf("schema_version = %q", got)
	}
	if err := validateSchemaObjects(ctx, raw, schema.Version2Statements[:1]); !errors.Is(err, ErrIncompatibleDatabase) {
		t.Fatalf("memory_records after the failed migration: %v", err)
	}
	session, err := (sessionRepository{db: raw}).Get(ctx, "v1-session")
	if err != nil || session.Version != 1 {
		t.Fatalf("session = %#v, %v", session, err)
	}
	replayed, err := (eventRepository{db: raw, begin: raw.BeginTx}).Replay(ctx, "run", 0, 10)
	if err != nil || len(replayed) != 1 || replayed[0].ID != "v1-event-1" {
		t.Fatalf("events = %#v, %v", replayed, err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("migration after the failed attempt: %v", err)
	}
	defer store.Close()
	assertCurrentSchema(t, store.db)
}

// Only absent schema evidence is an incompatible database. Busy, I/O and
// cancellation keep their cause, so the busy retry and ctx errors still work.
func TestSchemaReadFailureClassification(t *testing.T) {
	ioFailure := fmt.Errorf("%w: injected disk I/O error", tursogo.ErrTursoGeneric)
	notADatabase := fmt.Errorf("%w: injected", tursogo.ErrTursoNotADb)
	for _, tc := range []struct {
		name   string
		prefix string
		err    error
		want   error
		compat bool
	}{
		{"busy marker", "SELECT value FROM conduit_metadata", busyFault(), tursogo.ErrTursoBusy, false},
		{"I/O marker", "SELECT value FROM conduit_metadata", ioFailure, ioFailure, false},
		{"busy metadata lookup", "SELECT count(*) FROM sqlite_schema", busyFault(), tursogo.ErrTursoBusy, false},
		{"I/O definitions", "SELECT sql FROM sqlite_schema", ioFailure, ioFailure, false},
		{"not a database", "SELECT count(*) FROM sqlite_schema", notADatabase, tursogo.ErrTursoNotADb, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			store.Close()
			db, connector := openFaulty(t, path, 5000)
			connector.inject(tc.prefix, tc.err, -1)
			err = prepareExistingSchema(context.Background(), db)
			if !errors.Is(err, tc.want) || errors.Is(err, ErrIncompatibleDatabase) != tc.compat {
				t.Fatalf("prepare = %v", err)
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.db")
		store, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := prepareExistingSchema(ctx, store.db); !errors.Is(err, context.Canceled) || errors.Is(err, ErrIncompatibleDatabase) {
			t.Fatalf("prepare = %v", err)
		}
	})
}

// A busy marker read under the migration's write lock is retried like any
// other busy attempt instead of being reported as an incompatible database.
func TestBusyMarkerReadDuringMigrationIsRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	createV1Database(t, path).Close()
	db, connector := openFaulty(t, path, 5000)
	connector.inject("SELECT value FROM conduit_metadata", busyFault(), 1)
	if err := migrateSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if got := connector.count("BEGIN IMMEDIATE"); got != 2 {
		t.Fatalf("attempts = %d", got)
	}
}

// Validation of a current database that read successfully but could not roll
// back fails Open, and its connection is not reused.
func TestValidationWithFailedRollbackFailsAndDiscardsConnection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	db, connector := openFaulty(t, path, 5000)
	rollbackFailure := errors.New("injected rollback failure")
	connector.inject("ROLLBACK", rollbackFailure, -1)
	if err := prepareExistingSchema(ctx, db); !errors.Is(err, rollbackFailure) || errors.Is(err, ErrIncompatibleDatabase) {
		t.Fatalf("prepare = %v", err)
	}
	if got := connector.count("SELECT sql FROM sqlite_schema"); got != 1 {
		t.Fatalf("definition reads = %d", got)
	}
	if got := connector.closes.Load(); got != 1 {
		t.Fatalf("driver connections closed = %d", got)
	}
	if got := schemaVersion(t, db); got != "2" {
		t.Fatalf("schema_version = %q", got)
	}
	if got := connector.connects.Load(); got != 2 {
		t.Fatalf("driver connections opened = %d; the uncertain one was reused", got)
	}
}
