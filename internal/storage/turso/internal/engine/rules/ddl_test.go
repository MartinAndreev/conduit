package rules

import "testing"

// The stored forms are what tursogo v0.7.2 and v0.8.3 kept in sqlite_schema
// for the session store's v1 statements.
func TestEquivalentDDLAcceptsTursoStoredForm(t *testing.T) {
	for _, tc := range []struct{ stored, expected string }{
		{`CREATE TABLE conduit_metadata ("key" TEXT PRIMARY KEY, value TEXT NOT NULL)`, `CREATE TABLE conduit_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`},
		{`CREATE TABLE event_journal (run_id TEXT NOT NULL, sequence INTEGER NOT NULL, UNIQUE (run_id, sequence))`, "CREATE TABLE event_journal (\n\t\trun_id TEXT NOT NULL, sequence INTEGER NOT NULL,\n\t\tUNIQUE(run_id, sequence)\n\t)"},
		{`CREATE INDEX event_journal_replay ON event_journal (run_id, sequence)`, `CREATE INDEX event_journal_replay ON event_journal(run_id, sequence)`},
		{`CREATE TABLE t (x TEXT, "key"TEXT)`, `CREATE TABLE t (x TEXT, key TEXT)`},
		{`CREATE TABLE t ("key" TEXT)`, `CREATE TABLE t (key TEXT)`},
	} {
		if !EquivalentDDL(tc.stored, tc.expected) {
			t.Fatalf("%q is not %q", tc.stored, tc.expected)
		}
	}
}

func TestEquivalentDDLKeepsLiteralsAndQuotedNames(t *testing.T) {
	for _, tc := range []struct{ stored, expected string }{
		{`CREATE TABLE t (x TEXT CHECK (x IN ('a b')))`, `CREATE TABLE t (x TEXT CHECK (x IN ('ab')))`},
		{`CREATE TABLE t (x TEXT CHECK (x = 'key'))`, `CREATE TABLE t (x TEXT CHECK (x = key))`},
		{`CREATE TABLE "a b" (x TEXT)`, `CREATE TABLE "ab" (x TEXT)`},
		{`CREATE TABLE t (x TEXT CHECK (x IS "NULL"))`, `CREATE TABLE t (x TEXT CHECK (x IS NULL))`},
		{`CREATE TABLE t (x BOOLEAN DEFAULT "TRUE")`, `CREATE TABLE t (x BOOLEAN DEFAULT TRUE)`},
		{`CREATE TABLE t (x TEXT DEFAULT "CURRENT_TIMESTAMP")`, `CREATE TABLE t (x TEXT DEFAULT CURRENT_TIMESTAMP)`},
		{`CREATE TABLE t ("value" TEXT)`, `CREATE TABLE t (value TEXT)`},
		{`CREATE TABLE "a""b" (x TEXT)`, `CREATE TABLE "ab" (x TEXT)`},
		{`CREATE TABLE t ("1x" TEXT)`, `CREATE TABLE t (1x TEXT)`},
		{`CREATE TABLE t (x TEXTPRIMARYKEY)`, `CREATE TABLE t (x TEXT PRIMARY KEY)`},
		{`CREATE TABLE t (x TEXT, "key"TEXT)`, `CREATE TABLE t (x TEXT, keyTEXT)`},
	} {
		if EquivalentDDL(tc.stored, tc.expected) {
			t.Fatalf("%q accepted as %q", tc.stored, tc.expected)
		}
	}
}
