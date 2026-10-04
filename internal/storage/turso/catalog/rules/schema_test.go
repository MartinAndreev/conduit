package rules

import "testing"

func TestSchemaMarkerCompatibility(t *testing.T) {
	cases := []struct {
		key, actual, expected string
		kind                  SchemaKind
	}{
		{"application_id", "catalog", "catalog", SchemaCompatible},
		{"application_id", "sessions", "catalog", SchemaCorrupt},
		{"schema_version", "1", "1", SchemaCompatible},
		{"schema_version", "2", "1", SchemaFuture},
		{"schema_version", "0", "1", SchemaCorrupt},
		{"schema_version", "garbage", "1", SchemaCorrupt},
	}
	for _, tc := range cases {
		decision := DecideMarker(tc.key, tc.actual, tc.expected)
		if decision.Kind != tc.kind || decision.Reason == "" {
			t.Fatalf("%#v: %#v", tc, decision)
		}
	}
}
func TestDDLFormattingPreservesQuotedValuesAndConstraints(t *testing.T) {
	expected := `CREATE TABLE preferences (app TEXT CHECK(app IN ('tui', 'gui')), project TEXT REFERENCES projects(id))`
	formatted := `CREATE TABLE preferences(app TEXT CHECK ( app IN ( 'tui' , 'gui' ) ),project TEXT REFERENCES projects ( id ))`
	if decision := DecideTable(formatted, expected); decision.Kind != SchemaCompatible {
		t.Fatal(decision)
	}
	for _, changed := range []string{
		`CREATE TABLE preferences (app TEXT CHECK(app IN ('t ui', 'gui')), project TEXT REFERENCES projects(id))`,
		`CREATE TABLE preferences (app TEXT CHECK(app IN ('TUI', 'gui')), project TEXT REFERENCES projects(id))`,
		`CREATE TABLE preferences (app TEXT CHECK(app IN ('tui', 'gui')), project TEXT)`,
	} {
		if decision := DecideTable(changed, expected); decision.Kind != SchemaCorrupt {
			t.Fatal(decision)
		}
	}
	if DecideTable(`CREATE TABLE "a b" (x TEXT)`, `CREATE TABLE "ab" (x TEXT)`).Kind != SchemaCorrupt {
		t.Fatal("quoted identifiers merged")
	}
}

func TestDDLTokenBoundariesAreSignificant(t *testing.T) {
	expected := "CREATE TABLE projects(id TEXT PRIMARY KEY)"
	changed := "CREATE TABLE projects(id TEXTPRIMARYKEY)"
	if DecideTable(changed, expected).Kind != SchemaCorrupt {
		t.Fatal("custom type mistaken for primary key constraint")
	}
}
