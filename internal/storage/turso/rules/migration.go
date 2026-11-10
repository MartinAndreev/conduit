package rules

import (
	"fmt"
	"strconv"
)

type SchemaMigrationKind string

const (
	SchemaMigrationCurrent   SchemaMigrationKind = "current"
	SchemaMigrationMigrateV1 SchemaMigrationKind = "migrate_v1"
	SchemaMigrationFuture    SchemaMigrationKind = "future_schema"
	SchemaMigrationInvalid   SchemaMigrationKind = "invalid"
)

// SchemaMigrationFacts hold the schema_version marker exactly as read from an
// existing database and the version this build writes.
type SchemaMigrationFacts struct {
	StoredVersion  string
	CurrentVersion string
}

type SchemaMigrationDecision struct {
	Kind   SchemaMigrationKind
	Reason string
}

const migratableSchemaVersion = 1

// DecideSchemaMigration answers whether an existing database is used as is,
// migrated additively from v1, or refused. A newer schema is never touched.
// Markers that are not canonical positive integers are refused rather than
// guessed. The procedure evaluates it once before and again after taking the
// write lock, so a concurrent migrator's commit turns migrate into current.
func DecideSchemaMigration(f SchemaMigrationFacts) SchemaMigrationDecision {
	current, ok := parseSchemaVersion(f.CurrentVersion)
	if !ok {
		return SchemaMigrationDecision{Kind: SchemaMigrationInvalid, Reason: "build schema version is invalid"}
	}
	stored, ok := parseSchemaVersion(f.StoredVersion)
	if !ok {
		return SchemaMigrationDecision{Kind: SchemaMigrationInvalid, Reason: "invalid schema_version"}
	}
	switch {
	case stored > current:
		return SchemaMigrationDecision{Kind: SchemaMigrationFuture, Reason: fmt.Sprintf("schema_version %d is newer than supported %d", stored, current)}
	case stored == current:
		return SchemaMigrationDecision{Kind: SchemaMigrationCurrent, Reason: "schema_version is current"}
	case stored == migratableSchemaVersion && current == migratableSchemaVersion+1:
		return SchemaMigrationDecision{Kind: SchemaMigrationMigrateV1, Reason: "schema_version 1 is migrated additively to 2"}
	default:
		return SchemaMigrationDecision{Kind: SchemaMigrationInvalid, Reason: fmt.Sprintf("schema_version %d cannot be migrated to %d", stored, current)}
	}
}

func parseSchemaVersion(value string) (int, bool) {
	version, err := strconv.Atoi(value)
	if err != nil || version < 1 || strconv.Itoa(version) != value {
		return 0, false
	}
	return version, true
}

type MigrationRetryKind string

const (
	// MigrationRetryNotRetryable returns the attempt's failure unchanged.
	MigrationRetryNotRetryable MigrationRetryKind = "not_retryable"
	MigrationRetry             MigrationRetryKind = "retry"
	MigrationRetryCancelled    MigrationRetryKind = "cancelled"
	MigrationRetryExhausted    MigrationRetryKind = "exhausted"
)

// MigrationRetryFacts describe a failed migration attempt.
type MigrationRetryFacts struct {
	// Busy reports that the attempt itself failed because another connection
	// held the write lock; a cleanup failure is not part of it.
	Busy bool
	// CleanupFailed reports that ending the attempt's transaction or releasing
	// its connection failed, so the attempt's effect on the database is unknown.
	CleanupFailed bool
	Cancelled     bool
	// Attempts counts the attempts made so far, including the failed one.
	Attempts int
}

type MigrationRetryDecision struct {
	Kind   MigrationRetryKind
	Reason string
}

// MigrationMaxAttempts bounds the migration transactions, the first included;
// each already waits up to the connection busy timeout.
const MigrationMaxAttempts = 3

// DecideMigrationRetry answers whether a failed migration transaction is run
// again. Only a busy failure whose cleanup succeeded is repeated: the attempt
// then provably changed nothing. A cleanup failure is returned at once, before
// cancellation and the bound are considered. An invalid count is not retried.
func DecideMigrationRetry(f MigrationRetryFacts) MigrationRetryDecision {
	switch {
	case f.Attempts < 1:
		return MigrationRetryDecision{Kind: MigrationRetryNotRetryable, Reason: fmt.Sprintf("invalid attempt count %d", f.Attempts)}
	case f.CleanupFailed:
		return MigrationRetryDecision{Kind: MigrationRetryNotRetryable, Reason: "migration cleanup failed"}
	case !f.Busy:
		return MigrationRetryDecision{Kind: MigrationRetryNotRetryable, Reason: "failure is not a busy database"}
	case f.Cancelled:
		return MigrationRetryDecision{Kind: MigrationRetryCancelled, Reason: "migration was cancelled while the database was busy"}
	case f.Attempts >= MigrationMaxAttempts:
		return MigrationRetryDecision{Kind: MigrationRetryExhausted, Reason: fmt.Sprintf("database still busy after %d attempts", f.Attempts)}
	default:
		return MigrationRetryDecision{Kind: MigrationRetry, Reason: "database was busy and the attempt was rolled back"}
	}
}
