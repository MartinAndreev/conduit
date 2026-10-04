package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	domain "github.com/MartinAndreev/conduit/internal/projects"
)

// A private database is completely committed, checkpointed, closed and synced
// before create-if-absent publication. An existing empty file is corrupt; no
// opener mistakes another process's unfinished initialization for a fresh DB.
func (s Store) publishFirstRegistration(ctx context.Context, project domain.Project) (result app.InsertResult, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	directory := filepath.Dir(s.Path)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return result, fmt.Errorf("create catalog parent directory: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	private, err := os.MkdirTemp(directory, ".catalog-init-")
	if err != nil {
		return result, fmt.Errorf("prepare private catalog directory: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(private)) }()
	source := filepath.Join(private, "catalog.db")
	if err = prepareFirstDatabase(ctx, source, project); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	files := s.publicationFiles()
	if err = files.link(source, s.Path); err != nil {
		if errors.Is(err, os.ErrExist) {
			// The competing publication may contain a different project. Read and
			// insert into its completed schema; never replace its destination.
			missing, inspectErr := inspectCatalogFile(ctx, s.Path)
			if inspectErr != nil {
				return result, inspectErr
			}
			if missing {
				return result, errors.New("competing catalog disappeared before insertion")
			}
			return s.insertExisting(ctx, project)
		}
		return result, fmt.Errorf("publish catalog without replacement: %w", err)
	}
	result = app.InsertResult{Project: project, Persisted: true}
	return result, errors.Join(files.syncDirectory(directory), ctx.Err())
}

func prepareFirstDatabase(ctx context.Context, path string, project domain.Project) (err error) {
	db, conn, err := connectCatalog(ctx, path)
	if err != nil {
		return err
	}
	connectionClosed, databaseClosed := false, false
	defer func() {
		if !connectionClosed {
			err = errors.Join(err, conn.Close())
		}
		if !databaseClosed {
			err = errors.Join(err, db.Close())
		}
	}()
	if err = beginWrite(ctx, conn); err != nil {
		return err
	}
	completed := false
	defer func() {
		if !completed {
			err = errors.Join(err, rollback(conn))
		}
	}()
	if err = createSchema(ctx, conn); err != nil {
		return err
	}
	if _, err = insertProject(ctx, conn, project); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = commitCatalog(ctx, conn); err != nil {
		return fmt.Errorf("commit private catalog: %w", err)
	}
	completed = true
	// Publication includes only the main file. Its committed data must leave the
	// private WAL before linking, while no competing connection can write it.
	if _, err = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint private catalog: %w", err)
	}
	// Close before file syncing: all native handles and their writes finish here.
	err = conn.Close()
	connectionClosed = true
	if err != nil {
		return fmt.Errorf("close private catalog connection: %w", err)
	}
	err = db.Close()
	databaseClosed = true
	if err != nil {
		return fmt.Errorf("close private catalog database: %w", err)
	}
	return syncCatalogFile(path)
}

func syncCatalogFile(path string) (err error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open prepared catalog for sync: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync prepared catalog: %w", err)
	}
	return nil
}

// publicationFiles is the local filesystem boundary used to exercise failures
// immediately after real namespace publication without changing business flow.
type publicationFiles struct {
	link          func(string, string) error
	syncDirectory func(string) error
}

func (s Store) publicationFiles() publicationFiles {
	if s.publication != nil {
		return *s.publication
	}
	return publicationFiles{link: os.Link, syncDirectory: syncCatalogDirectory}
}
