package rules

import "testing"

func TestDecideSchemaMigration(t *testing.T) {
	for name, tc := range map[string]struct {
		facts SchemaMigrationFacts
		want  SchemaMigrationKind
	}{
		"current":                 {SchemaMigrationFacts{StoredVersion: "2", CurrentVersion: "2"}, SchemaMigrationCurrent},
		"v1":                      {SchemaMigrationFacts{StoredVersion: "1", CurrentVersion: "2"}, SchemaMigrationMigrateV1},
		"future":                  {SchemaMigrationFacts{StoredVersion: "3", CurrentVersion: "2"}, SchemaMigrationFuture},
		"v1 build on v2":          {SchemaMigrationFacts{StoredVersion: "2", CurrentVersion: "1"}, SchemaMigrationFuture},
		"empty":                   {SchemaMigrationFacts{StoredVersion: "", CurrentVersion: "2"}, SchemaMigrationInvalid},
		"zero":                    {SchemaMigrationFacts{StoredVersion: "0", CurrentVersion: "2"}, SchemaMigrationInvalid},
		"negative":                {SchemaMigrationFacts{StoredVersion: "-1", CurrentVersion: "2"}, SchemaMigrationInvalid},
		"not canonical":           {SchemaMigrationFacts{StoredVersion: "01", CurrentVersion: "2"}, SchemaMigrationInvalid},
		"padded":                  {SchemaMigrationFacts{StoredVersion: " 1", CurrentVersion: "2"}, SchemaMigrationInvalid},
		"text":                    {SchemaMigrationFacts{StoredVersion: "two", CurrentVersion: "2"}, SchemaMigrationInvalid},
		"v1 to non-adjacent":      {SchemaMigrationFacts{StoredVersion: "1", CurrentVersion: "3"}, SchemaMigrationInvalid},
		"invalid build beats all": {SchemaMigrationFacts{StoredVersion: "9", CurrentVersion: "x"}, SchemaMigrationInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			got := DecideSchemaMigration(tc.facts)
			if got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %#v, want %s", got, tc.want)
			}
		})
	}
}

func TestDecideMigrationRetry(t *testing.T) {
	for name, tc := range map[string]struct {
		facts MigrationRetryFacts
		want  MigrationRetryKind
	}{
		"busy":                       {MigrationRetryFacts{Busy: true, Attempts: 1}, MigrationRetry},
		"busy before bound":          {MigrationRetryFacts{Busy: true, Attempts: 2}, MigrationRetry},
		"busy at bound":              {MigrationRetryFacts{Busy: true, Attempts: 3}, MigrationRetryExhausted},
		"not busy":                   {MigrationRetryFacts{Attempts: 1}, MigrationRetryNotRetryable},
		"busy with failed cleanup":   {MigrationRetryFacts{Busy: true, CleanupFailed: true, Attempts: 1}, MigrationRetryNotRetryable},
		"cleanup beats cancellation": {MigrationRetryFacts{Busy: true, CleanupFailed: true, Cancelled: true, Attempts: 1}, MigrationRetryNotRetryable},
		"cleanup beats bound":        {MigrationRetryFacts{Busy: true, CleanupFailed: true, Attempts: 3}, MigrationRetryNotRetryable},
		"busy and cancelled":         {MigrationRetryFacts{Busy: true, Cancelled: true, Attempts: 1}, MigrationRetryCancelled},
		"cancellation beats bound":   {MigrationRetryFacts{Busy: true, Cancelled: true, Attempts: 3}, MigrationRetryCancelled},
		"cancelled without busy":     {MigrationRetryFacts{Cancelled: true, Attempts: 1}, MigrationRetryNotRetryable},
		"invalid attempt count":      {MigrationRetryFacts{Busy: true}, MigrationRetryNotRetryable},
	} {
		t.Run(name, func(t *testing.T) {
			got := DecideMigrationRetry(tc.facts)
			if got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %#v, want %s", got, tc.want)
			}
		})
	}
}
