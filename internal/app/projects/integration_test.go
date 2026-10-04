package projects_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	"github.com/MartinAndreev/conduit/internal/projectinfra"
	domain "github.com/MartinAndreev/conduit/internal/projects"
	"github.com/MartinAndreev/conduit/internal/storage/turso/catalog"
)

func TestRealCatalogRetainsMissingEntryAndContinues(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid")
	if err := os.Mkdir(valid, 0700); err != nil {
		t.Fatal(err)
	}
	store := catalog.Store{Path: filepath.Join(root, "catalog.db")}
	for _, p := range []domain.Project{{ID: "missing", Name: "a missing", CanonicalPath: filepath.Join(root, "missing")}, {ID: "valid", Name: "z valid", CanonicalPath: valid}} {
		if _, err := store.Insert(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	service := app.Service{Repository: store, Directories: projectinfra.Directories{}}
	got, err := service.ReadCatalog(t.Context())
	if len(got.Entries) != 2 || got.Entries[0].Directory.Kind != domain.Missing || got.Entries[1].Directory.Kind != domain.Available {
		t.Fatalf("catalog %+v, err %v", got, err)
	}
	if !errors.Is(err, os.ErrNotExist) || !errors.Is(got.Entries[0].Directory.Cause, os.ErrNotExist) {
		t.Fatalf("missing cause not preserved: %v %+v", err, got.Entries[0])
	}
	snapshot, err := store.Read(t.Context())
	if err != nil || len(snapshot.Projects) != 2 {
		t.Fatalf("registrations changed: %+v %v", snapshot, err)
	}
}

type refusedPreference struct{ app.Repository }

func (r refusedPreference) SetLastOpened(context.Context, domain.ApplicationID, domain.ID) (app.WriteResult, error) {
	return app.WriteResult{}, os.ErrPermission
}

func TestRealRegistrationRestoreAndPartialPreference(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	second := filepath.Join(root, "second")
	for _, path := range []string{project, second} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store := catalog.Store{Path: filepath.Join(root, "catalog.db")}
	s := app.Service{Repository: store, Directories: projectinfra.Directories{}}
	first, err := s.RegisterAndRecordOpened(t.Context(), domain.TUI, project, root)
	if err != nil || !first.Registration.Persisted || !first.Opened.Persisted {
		t.Fatalf("first %+v %v", first, err)
	}
	t.Run("symlink duplicate", func(t *testing.T) {
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(project, alias); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlink unavailable; requires Windows privilege: %v", err)
			}
			t.Fatal(err)
		}
		duplicate, err := s.RegisterLocalProject(t.Context(), alias, root)
		if err != nil || duplicate.Kind != domain.RegistrationExisting || duplicate.Project.ID != first.Registration.Project.ID {
			t.Fatalf("alias %+v %v", duplicate, err)
		}
	})
	gui, err := s.RegisterAndRecordOpened(t.Context(), domain.GUI, second, root)
	if err != nil {
		t.Fatal(err)
	}
	for appID, want := range map[domain.ApplicationID]domain.ID{domain.TUI: first.Registration.Project.ID, domain.GUI: gui.Registration.Project.ID} {
		restored, err := s.RestoreLastProject(t.Context(), appID)
		if err != nil || restored.Kind != app.RestoreReady || restored.Project.ID != want {
			t.Fatalf("restore %+v %v", restored, err)
		}
	}
	partialPath := filepath.Join(root, "partial")
	if err := os.Mkdir(partialPath, 0700); err != nil {
		t.Fatal(err)
	}
	failing := app.Service{Repository: refusedPreference{store}, Directories: projectinfra.Directories{}}
	partial, err := failing.RegisterAndRecordOpened(t.Context(), domain.TUI, partialPath, root)
	if !errors.Is(err, os.ErrPermission) || !partial.Registration.Persisted || partial.Opened.Persisted {
		t.Fatalf("partial %+v %v", partial, err)
	}
	retry, err := s.RegisterAndRecordOpened(t.Context(), domain.TUI, partialPath, root)
	if err != nil || retry.Registration.Project.ID != partial.Registration.Project.ID || !retry.Opened.Persisted {
		t.Fatalf("retry %+v %v", retry, err)
	}
}

func TestRealDanglingPreferenceHasTypedRestoreOutcome(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "catalog.db")
	store := catalog.Store{Path: path}
	if _, err := store.Insert(t.Context(), domain.Project{ID: "retained", Name: "project", CanonicalPath: directory}); err != nil {
		t.Fatal(err)
	}
	// Simulate an externally corrupted FK without changing normal adapter writes.
	db, err := sql.Open("turso", path)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(t.Context())
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(t.Context(), "PRAGMA foreign_keys = OFF"); err == nil {
		_, err = conn.ExecContext(t.Context(), "INSERT INTO last_opened (application_id, project_id) VALUES (?, ?)", "tui", "absent")
	}
	closeErr := errors.Join(conn.Close(), db.Close())
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	service := app.Service{Repository: store, Directories: projectinfra.Directories{}}
	got, err := service.RestoreLastProject(t.Context(), domain.TUI)
	if got.Kind != app.RestoreDangling || !errors.Is(err, app.ErrDanglingPreference) {
		t.Fatalf("dangling %+v %v", got, err)
	}
	gui, err := service.RestoreLastProject(t.Context(), domain.GUI)
	if err != nil || gui.Kind != app.RestoreNone {
		t.Fatalf("gui %+v %v", gui, err)
	}
	catalogRead, err := service.ReadCatalog(t.Context())
	if err != nil || len(catalogRead.Entries) != 1 {
		t.Fatalf("catalog %+v %v", catalogRead, err)
	}
}
