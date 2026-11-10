package turso

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/MartinAndreev/conduit/internal/storage/turso/rules"
	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
	tursogo "turso.tech/database/tursogo"
)

// migrateSchema applies the additive 1 -> 2 migration. A migrator that loses a
// race either waits for the winner's commit or receives a busy error and tries
// again; in both cases it then reads v2 and changes nothing. Repeating is safe
// only after the busy attempt was rolled back, which rules.DecideMigrationRetry
// requires: the transaction has no effect outside the database and decides
// again from the version it reads under the write lock.
func migrateSchema(ctx context.Context, db *sql.DB) error {
	for attempts := 1; ; attempts++ {
		failure, cleanup := migrateSchemaOnce(ctx, db)
		if failure == nil && cleanup == nil {
			return nil
		}
		err := errors.Join(failure, cleanup)
		decision := rules.DecideMigrationRetry(rules.MigrationRetryFacts{
			Busy:          errors.Is(failure, tursogo.ErrTursoBusy),
			CleanupFailed: cleanup != nil,
			Cancelled:     ctx.Err() != nil,
			Attempts:      attempts,
		})
		switch decision.Kind {
		case rules.MigrationRetry:
		case rules.MigrationRetryCancelled:
			return errors.Join(err, ctx.Err())
		case rules.MigrationRetryExhausted:
			return fmt.Errorf("migrate schema: %s: %w", decision.Reason, err)
		default:
			return err
		}
	}
}

// migrateSchemaOnce runs one transaction on a pinned connection and reports
// the attempt's failure separately from the failure to end the transaction or
// release the connection. BEGIN IMMEDIATE takes the write lock before the
// version marker is read again.
func migrateSchemaOnce(ctx context.Context, db *sql.DB) (failure, cleanup error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err), nil
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("reserve schema migration: %w", err), conn.Close()
	}
	committed, failure := migrateInTransaction(ctx, conn)
	if committed {
		return nil, conn.Close()
	}
	return failure, rollbackAndRelease(conn, "schema migration")
}

// migrateInTransaction reports whether COMMIT succeeded. Without a commit,
// including when another migrator already reached v2, the caller rolls back.
func migrateInTransaction(ctx context.Context, conn *sql.Conn) (bool, error) {
	decision, err := readSchemaMigrationDecision(ctx, conn)
	if err != nil {
		return false, err
	}
	switch decision.Kind {
	case rules.SchemaMigrationCurrent:
		return false, nil
	case rules.SchemaMigrationMigrateV1:
	default:
		return false, fmt.Errorf("%w: %s", ErrIncompatibleDatabase, decision.Reason)
	}
	if err := validateSchemaObjects(ctx, conn, schema.Version1Statements); err != nil {
		return false, err
	}
	for _, statement := range schema.Version2Statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return false, fmt.Errorf("migrate schema: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, "UPDATE conduit_metadata SET value = ? WHERE key = ?", schema.Version, "schema_version"); err != nil {
		return false, fmt.Errorf("record migrated schema version: %w", err)
	}
	// Cancellation must not leave an acknowledged commit unreported.
	if _, err := conn.ExecContext(context.WithoutCancel(ctx), "COMMIT"); err != nil {
		return false, fmt.Errorf("commit schema migration: %w", err)
	}
	return true, nil
}

// rollbackAndRelease ends the pinned connection's transaction and returns the
// connection to the pool. It has its own context: cancellation must not prevent
// rollback. If ROLLBACK fails, the transaction state is unknown, so the
// connection is discarded rather than handed to the next query of this pool.
func rollbackAndRelease(conn *sql.Conn, purpose string) error {
	if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		return errors.Join(fmt.Errorf("rollback %s: %w", purpose, err), discardConnection(conn))
	}
	return conn.Close()
}

// discardConnection closes the driver connection instead of pooling it:
// database/sql drops a connection whose Raw callback reports driver.ErrBadConn,
// and closing it ends the engine's transaction. database/sql does not report
// the driver's close error. Conn is then closed and must not be used.
func discardConnection(conn *sql.Conn) error {
	if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
		return fmt.Errorf("discard connection: %w", err)
	}
	return nil
}
