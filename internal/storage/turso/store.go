package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine"
	"github.com/MartinAndreev/conduit/internal/storage/turso/rules"
	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
	tursogo "turso.tech/database/tursogo"
)

var ErrIncompatibleDatabase = errors.New("incompatible conduit database")

// ErrDatabaseLocked reports that another process holds the database file and
// the engine refused shared access. It is not an incompatibility.
var ErrDatabaseLocked = errors.New("conduit database is locked by another process")

// ErrSharedMemoryInit reports that, with multi-process WAL enabled, another
// process was still creating the engine's <db>-tshm coordination file after
// the bounded retry. It says nothing about the database file itself.
var ErrSharedMemoryInit = errors.New("conduit database multi-process coordination file is not initialized")

// Options are the engine settings Open uses; the zero value is single-process.
type Options = engine.Options

type Store struct {
	db *sql.DB
}

// Open opens path with the default single-process options.
func Open(ctx context.Context, path string) (*Store, error) {
	return OpenWithOptions(ctx, path, Options{})
}

// OpenWithOptions opens path with explicit engine options. The plain path is
// what is inspected; engine parameters never come from it.
func OpenWithOptions(ctx context.Context, path string, options Options) (*Store, error) {
	connector, err := engine.NewConnector(path, options, 5000)
	if err != nil {
		return nil, fmt.Errorf("create Turso connector: %w", err)
	}
	info, statErr := os.Stat(path)
	existing := statErr == nil && info.Size() > 0
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect database: %w", statErr)
	}

	var db *sql.DB
	err = engine.RetrySharedMemoryInit(ctx, options, func() error {
		db = sql.OpenDB(foreignKeyConnector{Connector: connector})
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return nil, openFailure(err, existing)
	}
	store := &Store{db: db}
	if existing {
		if err := prepareExistingSchema(ctx, db); err != nil {
			db.Close()
			return nil, err
		}
		return store, nil
	}
	if err := createSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

// openFailure keeps a lock refusal and an unsettled coordination file distinct
// from an unreadable existing database.
func openFailure(err error, existing bool) error {
	switch {
	case engine.IsLockedByAnotherProcess(err):
		return fmt.Errorf("%w: %w", ErrDatabaseLocked, err)
	case errors.Is(err, engine.ErrSharedMemoryInit):
		return fmt.Errorf("%w: %w", ErrSharedMemoryInit, err)
	case existing:
		return fmt.Errorf("%w: cannot read existing file: %v", ErrIncompatibleDatabase, err)
	default:
		return fmt.Errorf("open Turso database: %w", err)
	}
}

// prepareExistingSchema validates an existing database and, for schema v1,
// performs the additive migration before validating the complete schema.
func prepareExistingSchema(ctx context.Context, db *sql.DB) error {
	decision, err := readSchemaMigrationDecision(ctx, db)
	if err != nil {
		return err
	}
	switch decision.Kind {
	case rules.SchemaMigrationCurrent:
	case rules.SchemaMigrationMigrateV1:
		if err := migrateSchema(ctx, db); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: %s", ErrIncompatibleDatabase, decision.Reason)
	}
	return validateCurrentSchema(ctx, db)
}

// validateCurrentSchema reads the stored definitions inside an explicit
// transaction on a pinned connection that ends before Open returns. With
// tursogo v0.7.2 an autocommit schema read left a snapshot open, so the
// connection's next write failed as stale after another connection committed.
// v0.8.3 no longer shows it; the bounded transaction keeps validation
// independent of that behavior. A failed rollback fails Open and discards the
// connection, as in the migration.
func validateCurrentSchema(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open schema validation connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return errors.Join(fmt.Errorf("begin schema validation: %w", err), conn.Close())
	}
	failure := validateSchemaObjects(ctx, conn, schema.Statements)
	return errors.Join(failure, rollbackAndRelease(conn, "schema validation"))
}

// readSchemaMigrationDecision checks the application marker before the
// version marker, so a foreign database is refused before anything else.
func readSchemaMigrationDecision(ctx context.Context, db database) (rules.SchemaMigrationDecision, error) {
	if err := requireMetadataTable(ctx, db); err != nil {
		return rules.SchemaMigrationDecision{}, err
	}
	application, err := readMarker(ctx, db, "application_id")
	if err != nil {
		return rules.SchemaMigrationDecision{}, err
	}
	if decision := rules.DecideSchemaMarker("application_id", application, schema.ApplicationID); decision.Kind != rules.SchemaCompatible {
		return rules.SchemaMigrationDecision{}, fmt.Errorf("%w: %s", ErrIncompatibleDatabase, decision.Reason)
	}
	version, err := readMarker(ctx, db, "schema_version")
	if err != nil {
		return rules.SchemaMigrationDecision{}, err
	}
	return rules.DecideSchemaMigration(rules.SchemaMigrationFacts{StoredVersion: version, CurrentVersion: schema.Version}), nil
}

// requireMetadataTable separates a database without Conduit's metadata table,
// which is incompatible, from a failure to read the schema, which is not.
func requireMetadataTable(ctx context.Context, db database) error {
	var tables int
	err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'conduit_metadata'").Scan(&tables)
	if err != nil {
		return schemaReadFailure("inspect conduit_metadata", err)
	}
	if tables == 0 {
		return fmt.Errorf("%w: missing conduit_metadata", ErrIncompatibleDatabase)
	}
	return nil
}

// readMarker treats only an absent or NULL marker as incompatible; other
// failures keep their cause so busy and cancellation stay recognizable.
func readMarker(ctx context.Context, db database, key string) (string, error) {
	var value sql.NullString
	err := db.QueryRowContext(ctx, "SELECT value FROM conduit_metadata WHERE key = ?", key).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("%w: invalid %s", ErrIncompatibleDatabase, key)
	case err != nil:
		return "", schemaReadFailure("read "+key, err)
	case !value.Valid:
		return "", fmt.Errorf("%w: invalid %s", ErrIncompatibleDatabase, key)
	}
	return value.String, nil
}

// validateSchemaObjects compares the stored DDL with the statements that must
// have created it. Column names alone would accept a table without its UNIQUE,
// FOREIGN KEY or CHECK constraints, or a missing index.
func validateSchemaObjects(ctx context.Context, db database, expected []string) error {
	stored, err := readSchemaDefinitions(ctx, db)
	if err != nil {
		return err
	}
	if decision := rules.DecideSchemaObjects(expected, stored); decision.Kind != rules.SchemaCompatible {
		return fmt.Errorf("%w: %s", ErrIncompatibleDatabase, decision.Reason)
	}
	return nil
}

func readSchemaDefinitions(ctx context.Context, db database) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL")
	if err != nil {
		return nil, schemaReadFailure("read schema definitions", err)
	}
	defer rows.Close()
	var definitions []string
	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil {
			return nil, schemaReadFailure("read schema definitions", err)
		}
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, schemaReadFailure("read schema definitions", err)
	}
	return definitions, nil
}

// schemaReadFailure keeps the cause of a failed schema read. A file the engine
// does not recognize as a database is incompatible, as it is when the first
// open fails; busy, cancellation and I/O failures are not.
func schemaReadFailure(action string, err error) error {
	if errors.Is(err, tursogo.ErrTursoNotADb) {
		return fmt.Errorf("%w: %s: %w", ErrIncompatibleDatabase, action, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func createSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	defer tx.Rollback()
	for _, statement := range schema.Statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO conduit_metadata (key, value) VALUES (?, ?)", "schema_version", schema.Version); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO conduit_metadata (key, value) VALUES (?, ?)", "application_id", schema.ApplicationID); err != nil {
		return fmt.Errorf("record application identity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema: %w", err)
	}
	return nil
}

func (s *Store) Sessions() sessions.Repository { return sessionRepository{db: s.db} }
func (s *Store) Events() events.Repository {
	return eventRepository{db: s.db, begin: s.db.BeginTx}
}

func (s *Store) WithinUnitOfWork(ctx context.Context, operation func(sessions.Writer, events.Appender) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin unit of work: %w", err)
	}
	defer tx.Rollback()
	if err := operation(sessionRepository{db: tx}, eventRepository{db: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit unit of work: %w", err)
	}
	return nil
}

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
