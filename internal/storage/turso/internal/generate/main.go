// Command generate creates Jet table and model code from the current initial
// bootstrap schema using Turso itself. The output directory is always explicit
// because Jet clears it before generation.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MartinAndreev/conduit/internal/storage/turso/schema"
	jetsqlite "github.com/go-jet/jet/v2/generator/sqlite"
	_ "turso.tech/database/tursogo"
)

func main() {
	output := flag.String("output", "", "dedicated destination for generated code")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "-output is required")
		os.Exit(2)
	}
	directory, err := os.MkdirTemp("", "conduit-turso-schema-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(directory)
	db, err := sql.Open("turso", filepath.Join(directory, "schema.db"))
	if err != nil {
		fail(err)
	}
	defer db.Close()
	for _, statement := range schema.Statements {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			fail(err)
		}
	}
	if err := jetsqlite.GenerateDB(db, *output); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
