package rules

import "testing"

func TestDecideConfigMigration(t *testing.T) {
	for name, tc := range map[string]struct {
		facts MigrationFacts
		want  MigrationKind
	}{
		"current":                {MigrationFacts{Valid: true, SchemaVersion: 2, CurrentSchemaVersion: 2}, MigrationCurrent},
		"v1":                     {MigrationFacts{Valid: true, SchemaVersion: 1, CurrentSchemaVersion: 2}, MigrationMigrateV1},
		"future read":            {MigrationFacts{FutureSchema: true, CurrentSchemaVersion: 2}, MigrationFutureSchema},
		"future valid facts":     {MigrationFacts{Valid: true, SchemaVersion: 3, CurrentSchemaVersion: 2}, MigrationFutureSchema},
		"future beats invalid":   {MigrationFacts{FutureSchema: true, Valid: false, CurrentSchemaVersion: 2}, MigrationFutureSchema},
		"invalid":                {MigrationFacts{SchemaVersion: 1, CurrentSchemaVersion: 2}, MigrationInvalid},
		"zero version":           {MigrationFacts{Valid: true, SchemaVersion: 0, CurrentSchemaVersion: 2}, MigrationInvalid},
		"v1 not adjacent source": {MigrationFacts{Valid: true, SchemaVersion: 1, CurrentSchemaVersion: 3}, MigrationInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			d := DecideConfigMigration(tc.facts)
			if d.Kind != tc.want || d.Reason == "" {
				t.Fatal(d)
			}
		})
	}
}

func TestDecideMigrationPublication(t *testing.T) {
	for _, tc := range []struct {
		facts MigrationPublicationFacts
		want  MigrationPublicationKind
	}{
		{MigrationPublicationFacts{}, PublishMigration},
		{MigrationPublicationFacts{Inconclusive: 1}, DeferMigration},
		{MigrationPublicationFacts{Unobserved: 1}, DeferMigration},
	} {
		d := DecideMigrationPublication(tc.facts)
		if d.Kind != tc.want || d.Reason == "" {
			t.Fatal(tc.facts, d)
		}
	}
}
