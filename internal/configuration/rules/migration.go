package rules

type MigrationKind string

const (
	MigrationCurrent      MigrationKind = "current"
	MigrationMigrateV1    MigrationKind = "migrate_v1"
	MigrationFutureSchema MigrationKind = "future_schema"
	MigrationInvalid      MigrationKind = "invalid"
)

// MigrationFacts describe an existing configuration file after it was read.
// SchemaVersion is meaningful only when Valid is true. CurrentSchemaVersion is
// supplied by the configuration owner; v1 is the only migration source.
type MigrationFacts struct {
	Valid                bool
	FutureSchema         bool
	SchemaVersion        int
	CurrentSchemaVersion int
}
type MigrationDecision struct {
	Kind   MigrationKind
	Reason string
}

const migratableSchemaVersion = 1

// DecideConfigMigration answers whether an existing file is used as is,
// migrated from v1, or rejected. A future schema takes precedence over
// validity and is never rewritten. Contradictory facts (a "valid" version this
// build does not define) are invalid rather than guessed.
func DecideConfigMigration(f MigrationFacts) MigrationDecision {
	if f.FutureSchema || (f.Valid && f.SchemaVersion > f.CurrentSchemaVersion) {
		return MigrationDecision{MigrationFutureSchema, "Configuration uses a newer schema; the file is left untouched."}
	}
	if !f.Valid {
		return MigrationDecision{MigrationInvalid, "Configuration is invalid; the file is left untouched."}
	}
	if f.SchemaVersion == f.CurrentSchemaVersion {
		return MigrationDecision{MigrationCurrent, "Configuration uses the current schema."}
	}
	if f.SchemaVersion == migratableSchemaVersion && f.CurrentSchemaVersion == migratableSchemaVersion+1 {
		return MigrationDecision{MigrationMigrateV1, "Configuration schema v1 is migrated to v2; only harnesses without an entry are discovered for it."}
	}
	return MigrationDecision{MigrationInvalid, "Configuration schema version is not supported; the file is left untouched."}
}

type MigrationPublicationKind string

const (
	PublishMigration MigrationPublicationKind = "publish_migration"
	DeferMigration   MigrationPublicationKind = "defer_migration"
)

// MigrationPublicationFacts count, among harnesses without a v1 entry, those
// with no observation and those whose discovery was inconclusive (probe
// failure or timeout).
type MigrationPublicationFacts struct {
	Unobserved   int
	Inconclusive int
}
type MigrationPublicationDecision struct {
	Kind   MigrationPublicationKind
	Reason string
}

// DecideMigrationPublication permits writing v2 only when discovery was
// conclusive for every harness the migration records. Otherwise migration is
// deferred: the v1 file stays unchanged and the next startup tries again.
func DecideMigrationPublication(f MigrationPublicationFacts) MigrationPublicationDecision {
	if f.Unobserved > 0 || f.Inconclusive > 0 {
		return MigrationPublicationDecision{DeferMigration, "Discovery was not conclusive for every new harness; the v1 configuration is kept and migration is retried on the next start."}
	}
	return MigrationPublicationDecision{PublishMigration, "Discovery was conclusive for every new harness."}
}
