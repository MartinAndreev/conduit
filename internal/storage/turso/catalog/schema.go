package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	"github.com/MartinAndreev/conduit/internal/storage/turso/catalog/rules"
	"github.com/MartinAndreev/conduit/internal/storage/turso/catalog/schema"
)

func validateSchema(ctx context.Context, conn *sql.Conn) error {
	for _, marker := range []struct{ key, value string }{{"application_id", schema.ApplicationID}, {"schema_version", schema.Version}} {
		var actual string
		if err := conn.QueryRowContext(ctx, "SELECT value FROM catalog_metadata WHERE key = ?", marker.key).Scan(&actual); err != nil {
			return errors.Join(app.ErrCorruptCatalog, fmt.Errorf("read catalog %s: %w", marker.key, err))
		}
		decision := rules.DecideMarker(marker.key, actual, marker.value)
		if decision.Kind == rules.SchemaFuture {
			return fmt.Errorf("%w: %s", app.ErrFutureCatalog, decision.Reason)
		}
		if decision.Kind != rules.SchemaCompatible {
			return fmt.Errorf("%w: %s", app.ErrCorruptCatalog, decision.Reason)
		}
	}
	names := []string{"catalog_metadata", "projects", "last_opened"}
	for index, name := range names {
		var actual string
		if err := conn.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = ?", name).Scan(&actual); err != nil {
			return errors.Join(app.ErrCorruptCatalog, fmt.Errorf("inspect catalog table %s: %w", name, err))
		}
		decision := rules.DecideTable(actual, schema.Statements[index])
		if decision.Kind != rules.SchemaCompatible {
			return fmt.Errorf("%w: %s: %s", app.ErrCorruptCatalog, name, decision.Reason)
		}
	}
	return nil
}

func createSchema(ctx context.Context, conn *sql.Conn) error {
	for _, statement := range schema.Statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create catalog schema: %w", err)
		}
	}
	for _, marker := range []struct{ key, value string }{{"application_id", schema.ApplicationID}, {"schema_version", schema.Version}} {
		if _, err := conn.ExecContext(ctx, "INSERT INTO catalog_metadata (key, value) VALUES (?, ?)", marker.key, marker.value); err != nil {
			return fmt.Errorf("write catalog %s: %w", marker.key, err)
		}
	}
	return nil
}
