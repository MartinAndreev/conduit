package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	app "github.com/MartinAndreev/conduit/internal/app/projects"
	domain "github.com/MartinAndreev/conduit/internal/projects"
	sessionsStore "github.com/MartinAndreev/conduit/internal/storage/turso"
	"github.com/MartinAndreev/conduit/internal/storage/turso/catalog/schema"
)

func project(index int) domain.Project {
	return domain.Project{ID: domain.ID(fmt.Sprintf("id-%d", index)), Name: "same-name", CanonicalPath: filepath.Join(os.TempDir(), "conduit-catalog-fixture-projects", fmt.Sprint(index))}
}

func TestMissingReadHasNoFilesystemEffects(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing", "catalog.db")
	store := Store{Path: path}
	snapshot, err := store.Read(context.Background())
	if err != nil || len(snapshot.Projects) != 0 || len(snapshot.LastOpened) != 0 {
		t.Fatalf("read = %#v %v", snapshot, err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent was created: %v", err)
	}
	if _, err := store.SetLastOpened(context.Background(), domain.TUI, "absent"); !errors.Is(err, app.ErrUnknownProject) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preference created parent: %v", err)
	}
}

func TestPersistenceDuplicateAndSeparatePreferences(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	store := Store{Path: path}
	first, err := store.Insert(ctx, project(1))
	if err != nil || !first.Persisted || first.Existing {
		t.Fatalf("first = %#v %v", first, err)
	}
	duplicate := project(1)
	duplicate.ID = "loser"
	duplicate.Name = "changed-name"
	existing, err := (Store{Path: path}).Insert(ctx, duplicate)
	if err != nil || !existing.Persisted || !existing.Existing || existing.Project != first.Project {
		t.Fatalf("duplicate = %#v %v", existing, err)
	}
	second, err := store.Insert(ctx, project(2))
	if err != nil {
		t.Fatal(err)
	}
	for application, id := range map[domain.ApplicationID]domain.ID{domain.TUI: first.Project.ID, domain.GUI: second.Project.ID} {
		result, err := (Store{Path: path}).SetLastOpened(ctx, application, id)
		if err != nil || !result.Persisted {
			t.Fatalf("preference = %#v %v", result, err)
		}
	}
	got, err := (Store{Path: path}).Read(ctx)
	if err != nil || len(got.Projects) != 2 || got.LastOpened[domain.TUI] != first.Project.ID || got.LastOpened[domain.GUI] != second.Project.ID {
		t.Fatalf("snapshot = %#v %v", got, err)
	}
	if _, err := store.SetLastOpened(ctx, domain.TUI, "unknown"); !errors.Is(err, app.ErrUnknownProject) {
		t.Fatal(err)
	}
	if _, err := store.SetLastOpened(ctx, "other", first.Project.ID); !errors.Is(err, app.ErrInvalidApplication) {
		t.Fatal(err)
	}
	after, err := store.Read(ctx)
	if err != nil || after.LastOpened[domain.TUI] != first.Project.ID {
		t.Fatalf("failed preference changed snapshot = %#v %v", after, err)
	}
}

func TestIndependentStoresConcurrentFirstRegistrations(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate-%v", duplicate), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			stores := []Store{{Path: path}, {Path: path}}
			var wg sync.WaitGroup
			start := make(chan struct{})
			results := make([]app.InsertResult, len(stores))
			errs := make([]error, len(stores))
			for index := range stores {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					<-start
					candidate := project(index)
					if duplicate {
						candidate.CanonicalPath = project(0).CanonicalPath
					}
					results[index], errs[index] = stores[index].Insert(context.Background(), candidate)
				}(index)
			}
			close(start)
			wg.Wait()
			for index, err := range errs {
				if err != nil || !results[index].Persisted {
					t.Fatalf("writer %d = %#v %v", index, results[index], err)
				}
			}
			snapshot, err := stores[0].Read(context.Background())
			want := 2
			if duplicate {
				want = 1
			}
			if err != nil || len(snapshot.Projects) != want {
				t.Fatalf("snapshot = %#v %v", snapshot, err)
			}
			if duplicate && results[0].Project.ID != results[1].Project.ID {
				t.Fatalf("different duplicate winners = %#v", results)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					t.Fatalf("private init directory leaked: %s", entry.Name())
				}
			}
		})
	}
}

func TestIndependentConnectionsConcurrentExistingRegistrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	if _, err := (Store{Path: path}).Insert(ctx, project(100)); err != nil {
		t.Fatal(err)
	}
	const count = 8
	start := make(chan struct{})
	failures := make(chan error, count)
	var wg sync.WaitGroup
	for index := 0; index < count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			candidate := project(index)
			candidate.CanonicalPath = project(index % 2).CanonicalPath
			_, err := (Store{Path: path}).Insert(ctx, candidate)
			failures <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := (Store{Path: path}).Read(ctx)
	if err != nil || len(snapshot.Projects) != 3 {
		t.Fatalf("snapshot = %#v %v", snapshot, err)
	}
}

func TestRejectExistingUnknownEmptyFuturePartialAndSessionWithoutMutation(t *testing.T) {
	cases := []struct {
		name     string
		prepare  func(*testing.T, string)
		expected error
	}{
		{"garbage", func(t *testing.T, p string) { write(t, p, []byte("not a database")) }, app.ErrCorruptCatalog},
		{"empty", func(t *testing.T, p string) { write(t, p, nil) }, app.ErrCorruptCatalog},
		{"foreign", func(t *testing.T, p string) { executeSQL(t, p, "CREATE TABLE foreign_data(value TEXT)") }, app.ErrCorruptCatalog},
		{"future", func(t *testing.T, p string) {
			seed(t, p)
			executeSQL(t, p, "UPDATE catalog_metadata SET value='2' WHERE key='schema_version'")
		}, app.ErrFutureCatalog},
		{"partial", func(t *testing.T, p string) { seed(t, p); executeSQL(t, p, "DROP TABLE last_opened") }, app.ErrCorruptCatalog},
		{"session", func(t *testing.T, p string) {
			db, err := sessionsStore.Open(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}, app.ErrCorruptCatalog},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			tc.prepare(t, path)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store := Store{Path: path}
			_, err = store.Read(context.Background())
			if !errors.Is(err, tc.expected) {
				t.Fatalf("read: %v", err)
			}
			_, err = store.Insert(context.Background(), project(99))
			if !errors.Is(err, tc.expected) {
				t.Fatalf("insert: %v", err)
			}
			_, err = store.SetLastOpened(context.Background(), domain.TUI, "id-99")
			if !errors.Is(err, tc.expected) {
				t.Fatalf("preference: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("rejected database bytes changed")
			}
		})
	}
}

func TestDanglingPreferenceRetainsCompleteCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	seed(t, path)
	executeSQL(t, path, "PRAGMA foreign_keys=OFF", "INSERT INTO last_opened VALUES ('tui', 'deleted')", "INSERT INTO last_opened VALUES ('gui', 'id-1')")
	snapshot, err := (Store{Path: path}).Read(context.Background())
	if err != nil || len(snapshot.Projects) != 1 || len(snapshot.LastOpened) != 2 || snapshot.LastOpened[domain.TUI] != "deleted" {
		t.Fatalf("snapshot = %#v %v", snapshot, err)
	}
	db, conn, err := connectCatalog(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "INSERT INTO last_opened VALUES ('other', 'id-1')"); err == nil {
		t.Fatal("invalid application accepted")
	}
	if _, err := conn.ExecContext(context.Background(), "UPDATE last_opened SET project_id='unknown' WHERE application_id='gui'"); err == nil {
		t.Fatal("foreign key accepted unknown project")
	}
}

func TestCancellationAndFailedPreferenceKeepCompletedRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "catalog.db")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	store := Store{Path: path}
	if _, err := store.Insert(canceled, project(1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled insert created parent: %v", err)
	}
	seed(t, path)
	if _, err := store.SetLastOpened(canceled, domain.TUI, "id-1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	snapshot, err := store.Read(context.Background())
	if err != nil || len(snapshot.Projects) != 1 || len(snapshot.LastOpened) != 0 {
		t.Fatalf("snapshot=%#v %v", snapshot, err)
	}
	duplicate, err := store.Insert(context.Background(), project(1))
	if err != nil || !duplicate.Existing {
		t.Fatalf("retry=%#v %v", duplicate, err)
	}
}

func TestApplicationSchemaCoverage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	seed(t, path)
	for _, application := range domain.Applications() {
		if _, err := (Store{Path: path}).SetLastOpened(context.Background(), application, "id-1"); err != nil {
			t.Fatalf("schema rejects domain application %s: %v", application, err)
		}
	}
	if len(domain.Applications()) != len(schema.ApplicationIDs) {
		t.Fatal("schema application membership differs from domain")
	}
	for _, stored := range schema.ApplicationIDs {
		if !domain.ValidApplication(domain.ApplicationID(stored)) {
			t.Fatalf("schema contains unsupported application %q", stored)
		}
	}
	if schema.Version != "1" {
		t.Fatal("catalog schema version requires explicit compatibility tests")
	}
}

func seed(t *testing.T, path string) {
	t.Helper()
	if result, err := (Store{Path: path}).Insert(context.Background(), project(1)); err != nil || !result.Persisted {
		t.Fatalf("seed=%#v %v", result, err)
	}
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
func executeSQL(t *testing.T, path string, queries ...string) {
	t.Helper()
	db, err := sql.Open("turso", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedExistingInsertionRollsBackWithoutPartialRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	seed(t, path)
	collision := project(2)
	collision.ID = "id-1"
	result, err := (Store{Path: path}).Insert(context.Background(), collision)
	if err == nil || result.Persisted {
		t.Fatalf("collision=%#v %v", result, err)
	}
	snapshot, err := (Store{Path: path}).Read(context.Background())
	if err != nil || len(snapshot.Projects) != 1 || snapshot.Projects[0].CanonicalPath != project(1).CanonicalPath {
		t.Fatalf("rollback=%#v %v", snapshot, err)
	}
	result, err = (Store{Path: path}).Insert(context.Background(), project(2))
	if err != nil || !result.Persisted {
		t.Fatalf("retry=%#v %v", result, err)
	}
}

func TestRejectCatalogSymlinkPreservesTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "real.db")
	seed(t, target)
	link := filepath.Join(filepath.Dir(target), "alias.db")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Store{Path: link}).Read(context.Background())
	if !errors.Is(err, app.ErrCorruptCatalog) {
		t.Fatal(err)
	}
	_, err = (Store{Path: link}).Insert(context.Background(), project(2))
	if !errors.Is(err, app.ErrCorruptCatalog) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("symlink target changed", err)
	}
}

func TestCompletedPublicationRemainsVisibleWithCancellationOrSyncFailure(t *testing.T) {
	for _, kind := range []string{"cancellation", "directory_sync"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("directory sync failed")
			source := publicationFiles{link: os.Link, syncDirectory: syncCatalogDirectory}
			expected := sentinel
			if kind == "cancellation" {
				expected = context.Canceled
				source.link = func(from, to string) error {
					err := os.Link(from, to)
					if err == nil {
						cancel()
					}
					return err
				}
			} else {
				source.syncDirectory = func(string) error { return sentinel }
			}
			result, err := (Store{Path: path, publication: &source}).Insert(ctx, project(1))
			if !errors.Is(err, expected) || !result.Persisted || result.Project != project(1) {
				t.Fatalf("completed effect=%#v %v", result, err)
			}
			retry, err := (Store{Path: path}).Insert(context.Background(), project(1))
			if err != nil || !retry.Existing || retry.Project.ID != result.Project.ID {
				t.Fatalf("retry=%#v %v", retry, err)
			}
		})
	}
}

func TestCancellationAtCommitPreservesAcknowledgedEffects(t *testing.T) {
	for _, operation := range []string{"registration", "preference"} {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			seed(t, path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := Store{Path: path, commit: func(ctx context.Context, conn *sql.Conn) error {
				// Cancellation arrives after the public pre-commit check, immediately
				// before the real driver commit. Native commit must finish un-canceled.
				cancel()
				return commitCatalog(ctx, conn)
			}}
			if operation == "registration" {
				result, err := store.Insert(ctx, project(2))
				if !errors.Is(err, context.Canceled) || !result.Persisted || result.Project != project(2) {
					t.Fatalf("commit=%#v %v", result, err)
				}
			} else {
				result, err := store.SetLastOpened(ctx, domain.TUI, "id-1")
				if !errors.Is(err, context.Canceled) || !result.Persisted {
					t.Fatalf("commit=%#v %v", result, err)
				}
			}
			snapshot, err := (Store{Path: path}).Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if operation == "registration" && len(snapshot.Projects) != 2 {
				t.Fatalf("committed registration lost: %#v", snapshot)
			}
			if operation == "preference" && snapshot.LastOpened[domain.TUI] != "id-1" {
				t.Fatalf("committed preference lost: %#v", snapshot)
			}
		})
	}
}
