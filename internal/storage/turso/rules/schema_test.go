package rules

import "testing"

func TestSchemaMarkerDecision(t *testing.T) {
	for _, test := range []struct {
		name, key, actual, expected string
		kind                        SchemaCompatibilityKind
		reason                      string
	}{
		{"application matches", "application_id", "conduit", "conduit", SchemaCompatible, "schema marker matches"},
		{"foreign application", "application_id", "foreign", "conduit", SchemaMarkerMismatch, "invalid application_id"},
		{"different version", "schema_version", "2", "1", SchemaMarkerMismatch, "invalid schema_version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := DecideSchemaMarker(test.key, test.actual, test.expected)
			if got.Kind != test.kind || got.Reason != test.reason {
				t.Fatalf("decision = %#v", got)
			}
		})
	}
}

func TestSchemaObjectsDecision(t *testing.T) {
	expected := []string{
		"CREATE TABLE event_journal (\n\t\trun_id TEXT NOT NULL, sequence INTEGER NOT NULL,\n\t\tUNIQUE(run_id, sequence)\n\t)",
		`CREATE INDEX event_journal_replay ON event_journal(run_id, sequence)`,
	}
	table := `CREATE TABLE event_journal (run_id TEXT NOT NULL, sequence INTEGER NOT NULL, UNIQUE (run_id, sequence))`
	index := `CREATE INDEX event_journal_replay ON event_journal (run_id, sequence)`
	for _, test := range []struct {
		name   string
		stored []string
		kind   SchemaCompatibilityKind
		reason string
	}{
		{"stored forms", []string{index, `CREATE TABLE extra (x TEXT)`, table}, SchemaCompatible, "schema objects match their definitions"},
		{"dropped unique", []string{`CREATE TABLE event_journal (run_id TEXT NOT NULL, sequence INTEGER NOT NULL)`, index}, SchemaObjectMismatch, "table event_journal is missing or differs from the expected definition"},
		{"missing index", []string{table}, SchemaObjectMismatch, "index event_journal_replay is missing or differs from the expected definition"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := DecideSchemaObjects(expected, test.stored)
			if got.Kind != test.kind || got.Reason != test.reason {
				t.Fatalf("decision = %#v", got)
			}
		})
	}
}
