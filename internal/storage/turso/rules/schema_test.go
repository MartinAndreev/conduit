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

func TestSchemaColumnsDecisionPreservesRequiredOrder(t *testing.T) {
	required := []string{"id", "revision", "version"}
	found := map[string]bool{"id": true, "extra": true}
	got := DecideSchemaColumns("sessions", required, found)
	if got.Kind != SchemaColumnMissing || got.Reason != "missing sessions.revision" {
		t.Fatalf("decision = %#v", got)
	}
	if len(found) != 2 || len(required) != 3 {
		t.Fatal("rule mutated facts")
	}
	found["revision"], found["version"] = true, true
	got = DecideSchemaColumns("sessions", required, found)
	if got.Kind != SchemaCompatible || got.Reason == "" {
		t.Fatalf("decision = %#v", got)
	}
}
