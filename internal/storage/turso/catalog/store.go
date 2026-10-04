// Package catalog implements the independent global catalog using embedded Turso.
// Connections remain private and each operation owns one pinned connection.
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	domain "github.com/MartinAndreev/conduit/internal/projects"
	tursogo "turso.tech/database/tursogo"
)

// Store is a lazy repository: construction and missing reads have no effects.
// A separate Store may safely address the same catalog from another process.
type Store struct {
	Path        string
	publication *publicationFiles
	commit      func(context.Context, *sql.Conn) error
}

var _ app.Repository = Store{}

func (s Store) Read(ctx context.Context) (snapshot app.Snapshot, err error) {
	snapshot = emptySnapshot()
	missing, err := inspectCatalogFile(ctx, s.Path)
	if err != nil || missing {
		return snapshot, err
	}
	db, conn, err := connectCatalog(ctx, s.Path)
	if err != nil {
		return snapshot, errors.Join(app.ErrCorruptCatalog, err)
	}
	defer func() { err = errors.Join(err, conn.Close(), db.Close()) }()
	if _, err = conn.ExecContext(ctx, "BEGIN"); err != nil {
		return snapshot, fmt.Errorf("begin catalog read: %w", err)
	}
	defer func() { err = errors.Join(err, rollback(conn)) }()
	if err = validateSchema(ctx, conn); err != nil {
		return snapshot, err
	}
	return readSnapshot(ctx, conn)
}

func (s Store) Insert(ctx context.Context, candidate domain.Project) (result app.InsertResult, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if candidate.ID == "" || candidate.Name == "" || !filepath.IsAbs(candidate.CanonicalPath) {
		return result, errors.New("catalog registration requires ID, name and absolute canonical path")
	}
	missing, err := inspectCatalogFile(ctx, s.Path)
	if err != nil {
		return result, err
	}
	if missing {
		return s.publishFirstRegistration(ctx, candidate)
	}
	return s.insertExisting(ctx, candidate)
}

func (s Store) insertExisting(ctx context.Context, candidate domain.Project) (result app.InsertResult, err error) {
	db, conn, err := connectCatalog(ctx, s.Path)
	if err != nil {
		return result, errors.Join(app.ErrCorruptCatalog, err)
	}
	defer func() { err = errors.Join(err, conn.Close(), db.Close()) }()
	if err = beginWrite(ctx, conn); err != nil {
		return result, err
	}
	completed := false
	defer func() {
		if !completed {
			err = errors.Join(err, rollback(conn))
		}
	}()
	if err = validateSchema(ctx, conn); err != nil {
		return result, err
	}
	result, err = insertProject(ctx, conn, candidate)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return app.InsertResult{}, err
	}
	if err = s.commitTransaction(ctx, conn); err != nil {
		return app.InsertResult{}, fmt.Errorf("commit catalog registration: %w", err)
	}
	completed = true
	result.Persisted = true
	return result, ctx.Err()
}

func (s Store) SetLastOpened(ctx context.Context, application domain.ApplicationID, project domain.ID) (result app.WriteResult, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if !domain.ValidApplication(application) {
		return result, app.ErrInvalidApplication
	}
	missing, err := inspectCatalogFile(ctx, s.Path)
	if err != nil {
		return result, err
	}
	if missing {
		return result, app.ErrUnknownProject
	}
	db, conn, err := connectCatalog(ctx, s.Path)
	if err != nil {
		return result, errors.Join(app.ErrCorruptCatalog, err)
	}
	defer func() { err = errors.Join(err, conn.Close(), db.Close()) }()
	if err = beginWrite(ctx, conn); err != nil {
		return result, err
	}
	completed := false
	defer func() {
		if !completed {
			err = errors.Join(err, rollback(conn))
		}
	}()
	if err = validateSchema(ctx, conn); err != nil {
		return result, err
	}
	if err = writePreference(ctx, conn, application, project); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = s.commitTransaction(ctx, conn); err != nil {
		return result, fmt.Errorf("commit last-opened preference: %w", err)
	}
	completed = true
	result.Persisted = true
	return result, ctx.Err()
}

func emptySnapshot() app.Snapshot {
	return app.Snapshot{Projects: []domain.Project{}, LastOpened: map[domain.ApplicationID]domain.ID{}}
}

func connectCatalog(ctx context.Context, path string) (*sql.DB, *sql.Conn, error) {
	connector, err := tursogo.NewConnector(path, tursogo.WithBusyTimeout(5000))
	if err != nil {
		return nil, nil, fmt.Errorf("create catalog connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("open catalog connection: %w", err), db.Close())
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("enable catalog foreign keys: %w", err), conn.Close(), db.Close())
	}
	var enabled int
	if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		return nil, nil, errors.Join(errors.New("catalog foreign key enforcement unavailable"), err, conn.Close(), db.Close())
	}
	return db, conn, nil
}

func beginWrite(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("reserve catalog write transaction: %w", err)
	}
	return nil
}

// Cleanup deliberately has its own context: cancellation must not prevent rollback.
func rollback(conn *sql.Conn) error {
	_, err := conn.ExecContext(context.Background(), "ROLLBACK")
	if err != nil {
		return fmt.Errorf("rollback catalog transaction: %w", err)
	}
	return nil
}

func inspectCatalogFile(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if path == "" || !filepath.IsAbs(path) {
		return false, errors.New("catalog path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if cancellation := ctx.Err(); cancellation != nil {
			return false, errors.Join(cancellation, err)
		}
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("inspect catalog file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return false, errors.Join(app.ErrCorruptCatalog, errors.New("catalog is not a nonempty regular database file"))
	}
	return false, ctx.Err()
}

// Commit is a bounded native effect with cancellation checked by the caller
// before entry. Cancellation cannot erase a driver-acknowledged durable write.
func commitCatalog(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(context.WithoutCancel(ctx), "COMMIT")
	return err
}
func (s Store) commitTransaction(ctx context.Context, conn *sql.Conn) error {
	if s.commit != nil {
		return s.commit(ctx, conn)
	}
	return commitCatalog(ctx, conn)
}
