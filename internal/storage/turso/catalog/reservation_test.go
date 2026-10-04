package catalog

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	_ "turso.tech/database/tursogo"
)

func TestWriteReservationSupport(t *testing.T) {
	db, err := sql.Open("turso", filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}

func TestClosedDatabaseMainFileContainsCommittedData(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	db, err := sql.Open("turso", source)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"CREATE TABLE proof(value TEXT)", "INSERT INTO proof VALUES ('committed')", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "published.db")
	if err := os.Link(source, destination); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("turso", destination)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var value string
	if err := reopened.QueryRow("SELECT value FROM proof").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "committed" {
		t.Fatal(value)
	}
}
