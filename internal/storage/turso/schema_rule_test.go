package turso

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
)

func TestRejectsWrongMarkerBeforeMissingTablesWithoutMutation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wrong-marker.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE conduit_metadata SET value = ? WHERE key = ?", "foreign", "application_id"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE event_journal"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(ctx, path)
	if !errors.Is(err, ErrIncompatibleDatabase) || err.Error() != "incompatible conduit database: invalid application_id" {
		t.Fatalf("open error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("validation mutated incompatible database")
	}
}

// createDatabase writes a database from statements with the given markers, as
// an older or foreign build could have left it.
func createDatabase(t *testing.T, path, version string, statements []string) {
	t.Helper()
	ctx := context.Background()
	db := openRaw(t, path, 5000)
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"schema_version": version, "application_id": schema.ApplicationID} {
		if _, err := db.ExecContext(ctx, "INSERT INTO conduit_metadata (key, value) VALUES (?, ?)", key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// replaced returns statements with old replaced in exactly one of them, or
// with the statement equal to old removed when replacement is empty.
func replaced(t *testing.T, statements []string, old, replacement string) []string {
	t.Helper()
	result := make([]string, 0, len(statements))
	changed := 0
	for _, statement := range statements {
		if statement == old && replacement == "" {
			changed++
			continue
		}
		if strings.Contains(statement, old) {
			changed++
			statement = strings.Replace(statement, old, replacement, 1)
		}
		result = append(result, statement)
	}
	if changed != 1 {
		t.Fatalf("%q matched %d statements", old, changed)
	}
	return result
}

// Matching column names are not enough: a database whose tables lack a
// constraint or an index is refused, before migrating and when current.
func TestRejectsSchemaWithDroppedConstraintWithoutMutation(t *testing.T) {
	v1, v2 := schema.Version1Statements, schema.Version2Statements
	for _, tc := range []struct {
		name       string
		version    string
		statements []string
	}{
		{"v1 without unique sequence", "1", replaced(t, v1, ",\n\t\tUNIQUE(run_id, sequence)", "")},
		{"v1 without replay index", "1", replaced(t, v1, v1[3], "")},
		{"v2 with v1 table missing unique sequence", "2", slices.Concat(replaced(t, v1, ",\n\t\tUNIQUE(run_id, sequence)", ""), v2)},
		{"v2 without source foreign key", "2", slices.Concat(v1, replaced(t, v2, " REFERENCES memory_record_versions (id)", ""))},
		{"v2 without accepted result check", "2", slices.Concat(v1, replaced(t, v2, ",\n\t\tCHECK (origin <> 'attempt' OR visibility <> 'project' OR accepted_result_ref IS NOT NULL)", ""))},
		{"v2 without review index", "2", slices.Concat(v1, replaced(t, v2, "CREATE INDEX memory_reviews_version ON memory_reviews (version_id)", ""))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			createDatabase(t, path, tc.version, tc.statements)
			before := fileContent(t, path)
			if _, err := Open(context.Background(), path); !errors.Is(err, ErrIncompatibleDatabase) {
				t.Fatalf("open = %v", err)
			}
			if fileContent(t, path) != before {
				t.Fatal("refusal mutated the database")
			}
		})
	}
}

// As in the project catalog, objects the schema does not define are not judged.
func TestAcceptsCurrentSchemaWithAdditionalObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	createDatabase(t, path, schema.Version, slices.Concat(schema.Statements, []string{`CREATE TABLE local_notes (note TEXT)`}))
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
}
