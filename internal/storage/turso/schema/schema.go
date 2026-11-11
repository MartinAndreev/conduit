//go:generate sh -c "cd ../internal/generate && go run . -output ../generated"

// Package schema owns the session database format. Version1Statements are the
// frozen v1 format; Version2Statements are the additive 1 -> 2 migration.
package schema

import "slices"

const Version = "2"

// Byte bounds of the memory_* and context_* tables' free-text columns, as
// their CHECKs state.
const (
	MaxIdentityBytes  = 256
	MaxTimestampBytes = 64
	MaxTextFieldBytes = 4096
	// MaxStoredContentBytes bounds a version's content and its search text
	// each: 4 × memory.MaxMaterialBytes. It is a storage cap, not the
	// admission limit: a version above memory.MaxMaterialBytes may be stored
	// but is never admitted. The write path rejects content whose normalized
	// search text would exceed it rather than truncating.
	MaxStoredContentBytes = 128 * 1024
)
const ApplicationID = "github.com/MartinAndreev/conduit"

// Version1Statements created schema v1. They must never change: the 1 -> 2
// migration and running v1 processes depend on them.
var Version1Statements = []string{
	`CREATE TABLE conduit_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	`CREATE TABLE sessions (
		id TEXT PRIMARY KEY, project_id TEXT NOT NULL, run_id TEXT NOT NULL,
		attempt_id TEXT NOT NULL, assignment_id TEXT NOT NULL, revision INTEGER NOT NULL,
		adapter_session_id TEXT NOT NULL, native_binding BLOB, state TEXT NOT NULL,
		version INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE event_journal (
		event_id TEXT PRIMARY KEY, run_id TEXT NOT NULL, sequence INTEGER NOT NULL,
		kind TEXT NOT NULL, payload_version INTEGER NOT NULL, payload BLOB NOT NULL,
		provenance TEXT NOT NULL, causation_id TEXT NOT NULL, observed_at TEXT NOT NULL,
		UNIQUE(run_id, sequence)
	)`,
	`CREATE INDEX event_journal_replay ON event_journal(run_id, sequence)`,
}

// Version2Statements add project memory tables only; they do not alter any v1
// table, so an already running v1 process keeps working on sessions and events.
//
// Enumerated CHECK lists mirror internal/memory and spec 012; a test keeps them
// in step with the domain owner. Absent optional values are NULL, never ”.
// The memory_records -> current version reference is deferred so a record and
// its first version can be inserted in one transaction. A memory_reviews row
// names who recorded it (actor) and the person or agent (reviewer); only a
// human approves or rejects, system and agent reviews are opinions.
//
// Every free-text memory_* and context_* column has a UTF-8 byte bound, so no
// read loads an unbounded value: identities, hashes and other metadata
// MaxIdentityBytes, timestamps MaxTimestampBytes, source refs and paths,
// review reasons and manifest item reasons MaxTextFieldBytes, content and
// search text MaxStoredContentBytes each. Columns bound by a foreign key to
// such a column need no bound of their own. The one exception is
// context_manifest_payloads.content, the whole composed context of one
// dispatch request: composition has no total byte limit yet (linked records
// are mandatory and not counted), so its bound is set with the payload write
// in task 4.3. A deletion receipt's subject_kind is memory.DeletionSubjectKinds.
//
// v2 is unreleased: databases created by an earlier v2 draft fail validation
// as incompatible and are not migrated.
var Version2Statements = []string{
	`CREATE TABLE memory_records (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		project_id TEXT NOT NULL CHECK (length(CAST(project_id AS BLOB)) BETWEEN 1 AND 256),
		category TEXT NOT NULL CHECK (length(CAST(category AS BLOB)) BETWEEN 1 AND 256),
		status TEXT NOT NULL CHECK (status IN ('proposed', 'active', 'needs_verification', 'rejected', 'superseded', 'archived')),
		current_version_id TEXT NOT NULL,
		version INTEGER NOT NULL CHECK (version >= 1),
		created_at TEXT NOT NULL CHECK (length(CAST(created_at AS BLOB)) BETWEEN 1 AND 64),
		updated_at TEXT NOT NULL CHECK (length(CAST(updated_at AS BLOB)) BETWEEN 1 AND 64),
		UNIQUE (id, project_id),
		FOREIGN KEY (id, current_version_id) REFERENCES memory_record_versions (record_id, id) DEFERRABLE INITIALLY DEFERRED
	)`,
	`CREATE INDEX memory_records_project_status ON memory_records (project_id, status)`,
	`CREATE TABLE memory_record_versions (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		record_id TEXT NOT NULL,
		project_id TEXT NOT NULL,
		content TEXT CHECK (length(CAST(content AS BLOB)) <= 131072),
		content_hash TEXT NOT NULL CHECK (length(CAST(content_hash AS BLOB)) BETWEEN 1 AND 256),
		search_text TEXT CHECK (length(CAST(search_text AS BLOB)) <= 131072),
		search_normalization_version INTEGER NOT NULL CHECK (search_normalization_version >= 1),
		content_deleted_at TEXT CHECK (length(CAST(content_deleted_at AS BLOB)) BETWEEN 1 AND 64),
		author TEXT NOT NULL CHECK (length(CAST(author AS BLOB)) BETWEEN 1 AND 256),
		trust TEXT NOT NULL CHECK (trust IN ('human_approved', 'conduit_observed', 'code_derived', 'agent_claim', 'external_import')),
		redaction_policy_version TEXT NOT NULL CHECK (length(CAST(redaction_policy_version AS BLOB)) BETWEEN 1 AND 256),
		visibility TEXT NOT NULL CHECK (visibility IN ('attempt', 'project')),
		origin TEXT NOT NULL CHECK (origin IN ('attempt', 'human')),
		run_id TEXT CHECK (length(CAST(run_id AS BLOB)) BETWEEN 1 AND 256),
		attempt_id TEXT CHECK (length(CAST(attempt_id AS BLOB)) BETWEEN 1 AND 256),
		base_commit TEXT CHECK (length(CAST(base_commit AS BLOB)) BETWEEN 1 AND 256),
		accepted_result_ref TEXT CHECK (length(CAST(accepted_result_ref AS BLOB)) BETWEEN 1 AND 256),
		created_at TEXT NOT NULL CHECK (length(CAST(created_at AS BLOB)) BETWEEN 1 AND 64),
		UNIQUE (record_id, id),
		UNIQUE (id, content_hash),
		FOREIGN KEY (record_id, project_id) REFERENCES memory_records (id, project_id),
		CHECK ((content_deleted_at IS NULL AND content IS NOT NULL AND search_text IS NOT NULL)
			OR (content_deleted_at IS NOT NULL AND content IS NULL AND search_text IS NULL)),
		CHECK ((origin = 'human' AND run_id IS NULL AND attempt_id IS NULL)
			OR (origin = 'attempt' AND run_id IS NOT NULL AND attempt_id IS NOT NULL AND base_commit IS NOT NULL)),
		CHECK (origin <> 'attempt' OR visibility <> 'project' OR accepted_result_ref IS NOT NULL)
	)`,
	`CREATE INDEX memory_record_versions_project_scan ON memory_record_versions (project_id, visibility)`,
	`CREATE TABLE memory_sources (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		version_id TEXT NOT NULL REFERENCES memory_record_versions (id),
		kind TEXT NOT NULL CHECK (kind IN ('spec', 'file', 'event', 'assignment')),
		ref TEXT CHECK (length(CAST(ref AS BLOB)) BETWEEN 1 AND 4096),
		path TEXT CHECK (length(CAST(path AS BLOB)) BETWEEN 1 AND 4096),
		blob_hash TEXT CHECK (length(CAST(blob_hash AS BLOB)) BETWEEN 1 AND 256),
		UNIQUE (version_id, path),
		CHECK ((kind = 'file' AND path IS NOT NULL AND blob_hash IS NOT NULL)
			OR (kind <> 'file' AND path IS NULL AND blob_hash IS NULL AND ref IS NOT NULL))
	)`,
	`CREATE TABLE memory_reviews (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		version_id TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		decision TEXT NOT NULL CHECK (decision IN ('approve', 'reject', 'opinion')),
		actor TEXT NOT NULL CHECK (actor IN ('human', 'system', 'agent')),
		reviewer TEXT NOT NULL CHECK (length(CAST(reviewer AS BLOB)) BETWEEN 1 AND 256),
		reason TEXT CHECK (length(CAST(reason AS BLOB)) BETWEEN 1 AND 4096),
		reviewed_at TEXT NOT NULL CHECK (length(CAST(reviewed_at AS BLOB)) BETWEEN 1 AND 64),
		FOREIGN KEY (version_id, content_hash) REFERENCES memory_record_versions (id, content_hash),
		CHECK (actor = 'human' OR decision = 'opinion')
	)`,
	`CREATE INDEX memory_reviews_version ON memory_reviews (version_id)`,
	`CREATE TABLE memory_relations (
		project_id TEXT NOT NULL,
		from_record_id TEXT NOT NULL,
		to_record_id TEXT NOT NULL,
		kind TEXT NOT NULL CHECK (kind IN ('supersedes', 'contradicts')),
		created_at TEXT NOT NULL CHECK (length(CAST(created_at AS BLOB)) BETWEEN 1 AND 64),
		PRIMARY KEY (from_record_id, to_record_id, kind),
		CHECK (from_record_id <> to_record_id),
		FOREIGN KEY (from_record_id, project_id) REFERENCES memory_records (id, project_id),
		FOREIGN KEY (to_record_id, project_id) REFERENCES memory_records (id, project_id)
	)`,
	`CREATE INDEX memory_relations_target ON memory_relations (to_record_id, kind)`,
	`CREATE TABLE context_manifests (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		project_id TEXT NOT NULL CHECK (length(CAST(project_id AS BLOB)) BETWEEN 1 AND 256),
		run_id TEXT NOT NULL CHECK (length(CAST(run_id AS BLOB)) BETWEEN 1 AND 256),
		attempt_id TEXT NOT NULL CHECK (length(CAST(attempt_id AS BLOB)) BETWEEN 1 AND 256),
		assignment_id TEXT NOT NULL CHECK (length(CAST(assignment_id AS BLOB)) BETWEEN 1 AND 256),
		revision INTEGER NOT NULL,
		intent_id TEXT NOT NULL CHECK (length(CAST(intent_id AS BLOB)) BETWEEN 1 AND 256),
		payload_hash TEXT NOT NULL CHECK (length(CAST(payload_hash AS BLOB)) BETWEEN 1 AND 256),
		redaction_policy_version TEXT NOT NULL CHECK (length(CAST(redaction_policy_version AS BLOB)) BETWEEN 1 AND 256),
		search_status TEXT NOT NULL CHECK (search_status IN ('ok', 'degraded', 'disabled')),
		created_at TEXT NOT NULL CHECK (length(CAST(created_at AS BLOB)) BETWEEN 1 AND 64),
		UNIQUE (project_id, run_id, attempt_id, assignment_id, revision, intent_id)
	)`,
	`CREATE TABLE context_manifest_items (
		manifest_id TEXT NOT NULL REFERENCES context_manifests (id),
		ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
		source_identity TEXT NOT NULL CHECK (length(CAST(source_identity AS BLOB)) BETWEEN 1 AND 256),
		source_version TEXT NOT NULL CHECK (length(CAST(source_version AS BLOB)) BETWEEN 1 AND 256),
		mode TEXT NOT NULL CHECK (mode IN ('included_text', 'referenced')),
		reason TEXT NOT NULL CHECK (length(CAST(reason AS BLOB)) BETWEEN 1 AND 4096),
		trust TEXT CHECK (trust IN ('human_approved', 'conduit_observed', 'code_derived', 'agent_claim', 'external_import')),
		PRIMARY KEY (manifest_id, ordinal)
	)`,
	`CREATE TABLE context_manifest_payloads (
		manifest_id TEXT NOT NULL PRIMARY KEY REFERENCES context_manifests (id),
		content TEXT,
		status TEXT NOT NULL CHECK (status IN ('present', 'removed_before_dispatch', 'removed_delivery_unknown', 'removed_after_delivery')),
		removed_at TEXT CHECK (length(CAST(removed_at AS BLOB)) BETWEEN 1 AND 64),
		CHECK ((status = 'present' AND content IS NOT NULL AND removed_at IS NULL)
			OR (status <> 'present' AND content IS NULL AND removed_at IS NOT NULL))
	)`,
	`CREATE TABLE context_deletion_receipts (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		project_id TEXT NOT NULL CHECK (length(CAST(project_id AS BLOB)) BETWEEN 1 AND 256),
		subject_kind TEXT NOT NULL CHECK (subject_kind IN ('memory_version', 'manifest_payload')),
		subject_id TEXT NOT NULL CHECK (length(CAST(subject_id AS BLOB)) BETWEEN 1 AND 256),
		content_hash TEXT NOT NULL CHECK (length(CAST(content_hash AS BLOB)) BETWEEN 1 AND 256),
		deleted_at TEXT NOT NULL CHECK (length(CAST(deleted_at AS BLOB)) BETWEEN 1 AND 64)
	)`,
	`CREATE INDEX context_deletion_receipts_project ON context_deletion_receipts (project_id, deleted_at)`,
}

// Statements is the complete current schema, used for fresh databases and code
// generation. A migrated v1 database executes the same Version2Statements.
var Statements = slices.Concat(Version1Statements, Version2Statements)
