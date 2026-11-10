package turso

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
)

// foreignKeyConnector enables foreign key enforcement on every connection the
// pool opens, because Turso, like SQLite, keeps the setting per connection and
// the memory tables rely on it.
type foreignKeyConnector struct{ driver.Connector }

func (c foreignKeyConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		return nil, errors.Join(errors.New("Turso connection cannot execute pragmas"), conn.Close())
	}
	if _, err := execer.ExecContext(ctx, "PRAGMA foreign_keys = ON", nil); err != nil {
		return nil, errors.Join(fmt.Errorf("enable foreign keys: %w", err), conn.Close())
	}
	return conn, nil
}
