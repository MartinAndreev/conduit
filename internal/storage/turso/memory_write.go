package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
)

// storedTimeLayout is UTC with a fixed-width nanosecond fraction, so the text
// of stored times sorts as the times do (spec 012 "Търсене"). It parses as
// time.RFC3339Nano.
const storedTimeLayout = "2006-01-02T15:04:05.000000000Z"

// formatStoredTime is the one format memory writes for its times.
func formatStoredTime(t time.Time) string {
	return t.UTC().Format(storedTimeLayout)
}

// withMemoryWrite runs operation on a pinned connection inside BEGIN
// IMMEDIATE, so the write lock is held from before the facts are read until
// the commit. As in the migration, a failed COMMIT leaves Turso's transaction
// open, so every path that does not commit rolls back explicitly; the commit
// is not cancelled once operation succeeded. purpose names the write in
// errors.
func withMemoryWrite(ctx context.Context, db *sql.DB, purpose string, operation func(*sql.Conn) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open %s connection: %w", purpose, err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return errors.Join(fmt.Errorf("begin %s: %w", purpose, err), conn.Close())
	}
	if err := operation(conn); err != nil {
		return errors.Join(err, rollbackAndRelease(conn, purpose))
	}
	if _, err := conn.ExecContext(context.WithoutCancel(ctx), "COMMIT"); err != nil {
		return errors.Join(fmt.Errorf("commit %s: %w", purpose, err), rollbackAndRelease(conn, purpose))
	}
	return conn.Close()
}

func execStatement(ctx context.Context, db database, action string, statement jet.Statement) error {
	query, args := statement.Sql()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

// execExpectingRows runs a guarded statement that must change exactly rows
// rows; otherwise it fails with diverged and the caller's transaction rolls
// back.
func execExpectingRows(ctx context.Context, db database, action string, statement jet.Statement, rows int64, diverged error) error {
	query, args := statement.Sql()
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: inspect result: %w", action, err)
	}
	if changed != rows {
		return fmt.Errorf("%s: %w", action, diverged)
	}
	return nil
}
