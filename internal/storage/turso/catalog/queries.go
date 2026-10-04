package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	domain "github.com/MartinAndreev/conduit/internal/projects"
	"github.com/MartinAndreev/conduit/internal/storage/turso/catalog/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

func readSnapshot(ctx context.Context, conn *sql.Conn) (app.Snapshot, error) {
	snapshot := emptySnapshot()
	query, args := jet.SELECT(table.Projects.AllColumns).FROM(table.Projects).ORDER_BY(table.Projects.Name.ASC(), table.Projects.CanonicalPath.ASC()).Sql()
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return snapshot, fmt.Errorf("read catalog projects: %w", err)
	}
	for rows.Next() {
		var project domain.Project
		if err := rows.Scan(&project.ID, &project.Name, &project.CanonicalPath); err != nil {
			return snapshot, errors.Join(fmt.Errorf("decode catalog project: %w", err), rows.Close())
		}
		if project.ID == "" || project.Name == "" || !filepath.IsAbs(project.CanonicalPath) {
			return snapshot, errors.Join(app.ErrCorruptCatalog, rows.Close())
		}
		snapshot.Projects = append(snapshot.Projects, project)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return snapshot, fmt.Errorf("read catalog projects: %w", err)
	}
	query, args = jet.SELECT(table.LastOpened.AllColumns).FROM(table.LastOpened).Sql()
	rows, err = conn.QueryContext(ctx, query, args...)
	if err != nil {
		return snapshot, fmt.Errorf("read catalog preferences: %w", err)
	}
	for rows.Next() {
		var application domain.ApplicationID
		var project domain.ID
		if err := rows.Scan(&application, &project); err != nil {
			return snapshot, errors.Join(fmt.Errorf("decode catalog preference: %w", err), rows.Close())
		}
		if !domain.ValidApplication(application) {
			return snapshot, errors.Join(app.ErrCorruptCatalog, rows.Close())
		}
		snapshot.LastOpened[application] = project
		// Retain the preference in the read model so RestoreLastProject can return a
		// typed dangling outcome; diagnostics must not silently delete it.
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return snapshot, fmt.Errorf("read catalog preferences: %w", err)
	}
	return snapshot, ctx.Err()
}

func insertProject(ctx context.Context, conn *sql.Conn, candidate domain.Project) (app.InsertResult, error) {
	insert := table.Projects.INSERT(table.Projects.AllColumns).VALUES(candidate.ID, candidate.Name, candidate.CanonicalPath).ON_CONFLICT(table.Projects.CanonicalPath).DO_NOTHING()
	query, args := insert.Sql()
	changed, err := conn.ExecContext(ctx, query, args...)
	if err != nil {
		return app.InsertResult{}, fmt.Errorf("insert catalog project: %w", err)
	}
	count, err := changed.RowsAffected()
	if err != nil {
		return app.InsertResult{}, fmt.Errorf("inspect catalog insertion: %w", err)
	}
	query, args = jet.SELECT(table.Projects.AllColumns).FROM(table.Projects).WHERE(table.Projects.CanonicalPath.EQ(jet.String(candidate.CanonicalPath))).Sql()
	var winner domain.Project
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&winner.ID, &winner.Name, &winner.CanonicalPath); err != nil {
		return app.InsertResult{}, fmt.Errorf("read winning catalog registration: %w", err)
	}
	return app.InsertResult{Project: winner, Existing: count == 0}, nil
}

func writePreference(ctx context.Context, conn *sql.Conn, application domain.ApplicationID, project domain.ID) error {
	query, args := jet.SELECT(table.Projects.ID).FROM(table.Projects).WHERE(table.Projects.ID.EQ(jet.String(string(project)))).Sql()
	var found string
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.ErrUnknownProject
		}
		return fmt.Errorf("check last-opened registration: %w", err)
	}
	statement := table.LastOpened.INSERT(table.LastOpened.AllColumns).VALUES(application, project).ON_CONFLICT(table.LastOpened.ApplicationID).DO_UPDATE(jet.SET(table.LastOpened.ProjectID.SET(table.LastOpened.EXCLUDED.ProjectID)))
	query, args = statement.Sql()
	if _, err := conn.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("write last-opened preference: %w", err)
	}
	return nil
}
