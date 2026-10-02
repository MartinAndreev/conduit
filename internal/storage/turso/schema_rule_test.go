package turso

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
