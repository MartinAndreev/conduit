package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/MartinAndreev/conduit/internal/storage/turso/rules"
	"os"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
	tursogo "turso.tech/database/tursogo"
)

var ErrIncompatibleDatabase = errors.New("incompatible conduit database")

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	info, statErr := os.Stat(path)
	existing := statErr == nil && info.Size() > 0
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect database: %w", statErr)
	}

	connector, err := tursogo.NewConnector(path, tursogo.WithBusyTimeout(5000))
	if err != nil {
		return nil, fmt.Errorf("create Turso connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		if existing {
			return nil, fmt.Errorf("%w: cannot read existing file: %v", ErrIncompatibleDatabase, err)
		}
		return nil, fmt.Errorf("open Turso database: %w", err)
	}
	if existing {
		if err := validateSchema(ctx, db); err != nil {
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

func validateSchema(ctx context.Context, db *sql.DB) error {
	markers := map[string]string{"application_id": schema.ApplicationID, "schema_version": schema.Version}
	for key, expected := range markers {
		var actual string
		err := db.QueryRowContext(ctx, "SELECT value FROM conduit_metadata WHERE key = ?", key).Scan(&actual)
		if err != nil {
			return fmt.Errorf("%w: invalid %s", ErrIncompatibleDatabase, key)
		}
		decision := rules.DecideSchemaMarker(key, actual, expected)
		if decision.Kind != rules.SchemaCompatible {
			return fmt.Errorf("%w: %s", ErrIncompatibleDatabase, decision.Reason)
		}
	}
	required := map[string][]string{
		"sessions":      {"id", "project_id", "run_id", "attempt_id", "assignment_id", "revision", "adapter_session_id", "native_binding", "state", "version", "created_at", "updated_at"},
		"event_journal": {"event_id", "run_id", "sequence", "kind", "payload_version", "payload", "provenance", "causation_id", "observed_at"},
	}
	for table, columns := range required {
		rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			return fmt.Errorf("%w: inspect %s", ErrIncompatibleDatabase, table)
		}
		found := make(map[string]bool)
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				rows.Close()
				return fmt.Errorf("%w: inspect %s columns", ErrIncompatibleDatabase, table)
			}
			found[column] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("%w: inspect %s columns: %v", ErrIncompatibleDatabase, table, err)
		}
		rows.Close()
		decision := rules.DecideSchemaColumns(table, columns, found)
		if decision.Kind != rules.SchemaCompatible {
			return fmt.Errorf("%w: %s", ErrIncompatibleDatabase, decision.Reason)
		}
	}
	return nil
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
