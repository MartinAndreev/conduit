//go:generate sh -c "cd ../internal/generate && go run . -output ../generated"

// Package schema owns the session database format. Version1Statements are the
// frozen v1 format; Version2Statements are the additive 1 -> 2 migration.
package schema

import "slices"

const Version = "2"

// Byte bounds of the memory_* and context_* tables' free-text columns, as
// their CHECKs state. MaxIdentityBytes and MaxTextFieldBytes equal
// memory.MaxIdentityBytes and memory.MaxTextFieldBytes, which the write
// path's rules refuse above; a test keeps them equal.
const (
	MaxIdentityBytes  = 256
	MaxTimestampBytes = 64
	MaxTextFieldBytes = 4096
	// MaxStoredContentBytes bounds a version's content and its search text
	// each. It equals memory.MaxStoredContentBytes, which the write path's
	// rule refuses above rather than truncating; a test keeps the two equal.
	MaxStoredContentBytes = 128 * 1024
	// MaxManifestPayloadBytes bounds context_manifest_payloads.content. It
	// equals execution.MaxManifestPayloadBytes; a test keeps the two equal.
	MaxManifestPayloadBytes = 1024 * 1024
)

// BindingLockedMessage is the text sessions_binding_locked raises. The
// session repository recognizes the refusal by it.
const BindingLockedMessage = "conduit: session binding locked by an open dispatch intent"
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

// Version2Statements add the project memory and dispatch tables; they do not
// alter any v1 table, so an already running v1 process keeps working on
// sessions and events. The one v2 object on a v1 table is the trigger
// sessions_binding_locked: while a session has an open dispatch intent
// (recorded, dispatching, unknown, diverged) no writer, a v1 process included,
// may change the session's binding (project, run, attempt, assignment,
// revision, adapter session). It refuses such an UPDATE with BindingLockedMessage
// and does not judge any other column. The partial unique index
// dispatch_intents_one_open keeps at most one open intent per session. The
// guarantee holds for processes that open the database in the same access
// mode; the mixed multi-process WAL mode is outside it (spec 012).
//
// Enumerated CHECK lists mirror internal/memory and spec 012; a test keeps them
// in step with the domain owner. Absent optional values are NULL, never ”.
// The memory_records -> current version reference is deferred so a record and
// its first version can be inserted in one transaction. A memory_reviews row
// names who recorded it (actor) and the person or agent (reviewer); only a
// human approves, rejects or dismisses, system and agent reviews are
// opinions. Only an agent opinion carries a suggestion (memory.Suggestions,
// task 5.5), and a human dismissal always carries the dismissed one (Martin,
// 7а, 2026-11-15).
//
// Every free-text memory_* and context_* column has a UTF-8 byte bound, so no
// read loads an unbounded value: identities, hashes and other metadata
// MaxIdentityBytes, timestamps MaxTimestampBytes, source refs and paths,
// review reasons and manifest item reasons MaxTextFieldBytes, content and
// search text MaxStoredContentBytes each. Columns bound by a foreign key to
// such a column need no bound of their own. context_manifest_payloads.content,
// the whole composed context of one dispatch request, is bounded by
// MaxManifestPayloadBytes, the product limit execution.MaxManifestPayloadBytes
// refuses above (spec 012, task 2.1, Martin 2026-11-16). A deletion receipt's
// subject_kind is memory.DeletionSubjectKinds.
//
// v2 is unreleased and was changed in place again for task 2.1 (dispatch
// intent, Martin 2026-11-16): databases created by an earlier v2 draft fail
// validation as incompatible and are not migrated; such a file is deleted by
// hand and created again.
var Version2Statements = []string{
	`CREATE TABLE memory_records (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		project_id TEXT NOT NULL CHECK (length(CAST(project_id AS BLOB)) BETWEEN 1 AND 256),
		category TEXT NOT NULL CHECK (category IN ('decision', 'convention', 'constraint', 'fact', 'gotcha')),
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
		decision TEXT NOT NULL CHECK (decision IN ('approve', 'reject', 'opinion', 'dismiss')),
		actor TEXT NOT NULL CHECK (actor IN ('human', 'system', 'agent')),
		reviewer TEXT NOT NULL CHECK (length(CAST(reviewer AS BLOB)) BETWEEN 1 AND 256),
		reason TEXT CHECK (length(CAST(reason AS BLOB)) BETWEEN 1 AND 4096),
		reviewed_at TEXT NOT NULL CHECK (length(CAST(reviewed_at AS BLOB)) BETWEEN 1 AND 64),
		suggestion TEXT CHECK (suggestion IN ('archive', 'delete')),
		FOREIGN KEY (version_id, content_hash) REFERENCES memory_record_versions (id, content_hash),
		CHECK (actor = 'human' OR decision = 'opinion'),
		CHECK (suggestion IS NULL OR (decision = 'opinion' AND actor = 'agent') OR decision = 'dismiss'),
		CHECK (decision <> 'dismiss' OR (actor = 'human' AND suggestion IS NOT NULL))
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
	`CREATE TABLE dispatch_intents (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		kind TEXT NOT NULL CHECK (kind IN ('start_turn', 'steer')),
		session_id TEXT NOT NULL REFERENCES sessions (id),
		session_sequence INTEGER NOT NULL CHECK (typeof(session_sequence) = 'integer' AND session_sequence >= 1),
		adapter_session_id TEXT NOT NULL CHECK (length(CAST(adapter_session_id AS BLOB)) BETWEEN 1 AND 256),
		project_id TEXT NOT NULL CHECK (length(CAST(project_id AS BLOB)) BETWEEN 1 AND 256),
		run_id TEXT NOT NULL CHECK (length(CAST(run_id AS BLOB)) BETWEEN 1 AND 256),
		attempt_id TEXT NOT NULL CHECK (length(CAST(attempt_id AS BLOB)) BETWEEN 1 AND 256),
		assignment_id TEXT NOT NULL CHECK (length(CAST(assignment_id AS BLOB)) BETWEEN 1 AND 256),
		expected_revision INTEGER NOT NULL CHECK (typeof(expected_revision) = 'integer' AND expected_revision >= 1),
		target_revision INTEGER NOT NULL CHECK (typeof(target_revision) = 'integer' AND target_revision >= 1),
		expected_turn_id TEXT CHECK (length(CAST(expected_turn_id AS BLOB)) BETWEEN 1 AND 256),
		delivery_mode TEXT CHECK (delivery_mode IN ('active_turn', 'next_boundary')),
		request_hash TEXT NOT NULL CHECK (length(CAST(request_hash AS BLOB)) = 64),
		memory TEXT NOT NULL CHECK (memory IN ('none', 'with_manifest')),
		state TEXT NOT NULL CHECK (state IN ('recorded', 'dispatching', 'accepted', 'rejected', 'unknown', 'diverged', 'invalidated')),
		version INTEGER NOT NULL CHECK (typeof(version) = 'integer' AND version >= 1),
		created_at TEXT NOT NULL CHECK (length(CAST(created_at AS BLOB)) BETWEEN 1 AND 64),
		claimed_at TEXT CHECK (length(CAST(claimed_at AS BLOB)) BETWEEN 1 AND 64),
		settled_at TEXT CHECK (length(CAST(settled_at AS BLOB)) BETWEEN 1 AND 64),
		invalidated_at TEXT CHECK (length(CAST(invalidated_at AS BLOB)) BETWEEN 1 AND 64),
		UNIQUE (session_id, session_sequence),
		CHECK ((kind = 'start_turn' AND target_revision = expected_revision
				AND expected_turn_id IS NULL AND delivery_mode IS NULL)
			OR (kind = 'steer' AND expected_revision <= 9223372036854775806
				AND target_revision = expected_revision + 1
				AND expected_turn_id IS NOT NULL AND delivery_mode IS NOT NULL)),
		CHECK ((state = 'recorded' AND claimed_at IS NULL AND settled_at IS NULL AND invalidated_at IS NULL)
			OR (state = 'dispatching' AND claimed_at IS NOT NULL AND settled_at IS NULL AND invalidated_at IS NULL)
			OR (state IN ('accepted', 'rejected', 'unknown', 'diverged') AND claimed_at IS NOT NULL AND settled_at IS NOT NULL AND invalidated_at IS NULL)
			OR (state = 'invalidated' AND claimed_at IS NULL AND settled_at IS NULL AND invalidated_at IS NOT NULL))
	)`,
	`CREATE INDEX dispatch_intents_session_state ON dispatch_intents (session_id, state)`,
	`CREATE UNIQUE INDEX dispatch_intents_one_open ON dispatch_intents (session_id)
		WHERE state IN ('recorded', 'dispatching', 'unknown', 'diverged')`,
	`CREATE TRIGGER sessions_binding_locked BEFORE UPDATE OF project_id, run_id, attempt_id, assignment_id, revision, adapter_session_id ON sessions
		WHEN (NEW.project_id IS NOT OLD.project_id OR NEW.run_id IS NOT OLD.run_id
				OR NEW.attempt_id IS NOT OLD.attempt_id OR NEW.assignment_id IS NOT OLD.assignment_id
				OR NEW.revision IS NOT OLD.revision OR NEW.adapter_session_id IS NOT OLD.adapter_session_id)
			AND EXISTS (SELECT 1 FROM dispatch_intents
				WHERE session_id = OLD.id AND state IN ('recorded', 'dispatching', 'unknown', 'diverged'))
		BEGIN SELECT RAISE(ABORT, 'conduit: session binding locked by an open dispatch intent'); END`,
	`CREATE TABLE dispatch_outcomes (
		intent_id TEXT NOT NULL PRIMARY KEY REFERENCES dispatch_intents (id),
		outcome TEXT NOT NULL CHECK (outcome IN ('accepted', 'rejected', 'unknown')),
		classification TEXT NOT NULL CHECK (classification IN ('turn_started', 'advanced', 'insufficient_evidence',
			'transport_only', 'rejected_receipt', 'delivery_unknown', 'not_sent', 'possibly_sent',
			'accepted_without_result', 'unclassified_error', 'correlation_mismatch', 'invalid_observation')),
		observation TEXT NOT NULL CHECK (observation IN ('start_result', 'receipt', 'provider_error', 'other_error', 'invalid')),
		invalid_field TEXT CHECK (invalid_field IN ('contradictory', 'turn', 'targeting', 'result_enforcement',
			'disposition', 'receipt_knowledge', 'native_receipt_id', 'operation', 'error_code', 'remote_effect')),
		result_enforcement TEXT CHECK (result_enforcement IN ('native_schema', 'prompt_only', 'none')),
		disposition TEXT CHECK (disposition IN ('transport_accepted', 'queued_by_harness', 'rejected', 'delivery_unknown')),
		receipt_knowledge TEXT CHECK (receipt_knowledge IN ('native_receipt_known', 'no_native_receipt', 'unknown')),
		targeting TEXT CHECK (targeting IN ('native_turn_compare_and_swap', 'native_turn_id', 'session_scope_with_adapter_lease', 'unknown')),
		error_code TEXT CHECK (error_code IN ('capability_unsupported', 'capability_unknown', 'stale_binding',
			'native_target_rejected', 'delivery_rejected', 'delivery_unknown', 'session_lost', 'turn_lost',
			'protocol_violation', 'event_gap', 'backpressure_exceeded', 'auth_failed', 'executable_missing',
			'deadline', 'caller_cancelled')),
		remote_effect TEXT CHECK (remote_effect IN ('not_sent', 'possibly_sent', 'accepted', 'unknown')),
		dispatched_for TEXT CHECK (dispatched_for IN ('absent', 'matches', 'differs')),
		native_receipt_id TEXT CHECK (length(CAST(native_receipt_id AS BLOB)) BETWEEN 1 AND 256),
		adapter_turn_id TEXT CHECK (length(CAST(adapter_turn_id AS BLOB)) BETWEEN 1 AND 256),
		advanced INTEGER NOT NULL CHECK (advanced IN (0, 1)),
		divergence TEXT CHECK (divergence IN ('binding_changed', 'revision_changed', 'session_missing')),
		observed_at TEXT NOT NULL CHECK (length(CAST(observed_at AS BLOB)) BETWEEN 1 AND 64),
		CHECK ((observation = 'invalid') = (invalid_field IS NOT NULL)),
		CHECK (observation = 'receipt' OR (disposition IS NULL AND receipt_knowledge IS NULL
			AND dispatched_for IS NULL AND native_receipt_id IS NULL)),
		CHECK (observation <> 'receipt' OR (disposition IS NOT NULL AND receipt_knowledge IS NOT NULL
			AND targeting IS NOT NULL AND dispatched_for IS NOT NULL)),
		CHECK (observation IN ('start_result', 'invalid') OR adapter_turn_id IS NULL),
		CHECK (observation IN ('start_result', 'invalid') OR result_enforcement IS NULL),
		CHECK (observation <> 'start_result' OR result_enforcement IS NOT NULL),
		CHECK (observation NOT IN ('receipt', 'start_result') OR targeting IS NOT NULL),
		CHECK (observation IN ('receipt', 'start_result', 'invalid') OR targeting IS NULL),
		CHECK ((observation = 'provider_error') = (error_code IS NOT NULL AND remote_effect IS NOT NULL)),
		CHECK (observation = 'provider_error' OR (error_code IS NULL AND remote_effect IS NULL)),
		CHECK (classification <> 'turn_started' OR (observation = 'start_result' AND adapter_turn_id IS NOT NULL)),
		CHECK (classification <> 'advanced' OR (observation = 'receipt' AND dispatched_for = 'matches')),
		CHECK (advanced = 0 OR (classification = 'advanced' AND divergence IS NULL)),
		CHECK (divergence IS NULL OR (classification = 'advanced' AND advanced = 0)),
		CHECK ((observation = 'invalid') = (classification = 'invalid_observation')),
		CHECK ((outcome = 'accepted' AND classification IN ('turn_started', 'advanced'))
			OR (outcome = 'rejected' AND classification IN ('rejected_receipt', 'not_sent'))
			OR (outcome = 'unknown' AND classification IN ('insufficient_evidence', 'transport_only', 'delivery_unknown',
				'possibly_sent', 'accepted_without_result', 'unclassified_error', 'correlation_mismatch', 'invalid_observation')))
	)`,
	`CREATE TABLE context_manifests (
		id TEXT NOT NULL PRIMARY KEY CHECK (length(CAST(id AS BLOB)) BETWEEN 1 AND 256),
		project_id TEXT NOT NULL CHECK (length(CAST(project_id AS BLOB)) BETWEEN 1 AND 256),
		run_id TEXT NOT NULL CHECK (length(CAST(run_id AS BLOB)) BETWEEN 1 AND 256),
		attempt_id TEXT NOT NULL CHECK (length(CAST(attempt_id AS BLOB)) BETWEEN 1 AND 256),
		assignment_id TEXT NOT NULL CHECK (length(CAST(assignment_id AS BLOB)) BETWEEN 1 AND 256),
		revision INTEGER NOT NULL CHECK (typeof(revision) = 'integer' AND revision >= 1),
		intent_id TEXT NOT NULL UNIQUE REFERENCES dispatch_intents (id),
		composition TEXT NOT NULL CHECK (composition IN ('composed', 'empty')),
		payload_hash TEXT NOT NULL CHECK (length(CAST(payload_hash AS BLOB)) = 64),
		manifest_hash TEXT NOT NULL CHECK (length(CAST(manifest_hash AS BLOB)) = 64),
		redaction_policy_version TEXT CHECK (length(CAST(redaction_policy_version AS BLOB)) BETWEEN 1 AND 256),
		search_status TEXT NOT NULL CHECK (search_status IN ('ok', 'degraded', 'disabled')),
		created_at TEXT NOT NULL CHECK (length(CAST(created_at AS BLOB)) BETWEEN 1 AND 64),
		CHECK ((composition = 'composed' AND redaction_policy_version IS NOT NULL)
			OR (composition = 'empty' AND redaction_policy_version IS NULL
				AND payload_hash = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'))
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
		content TEXT CHECK (length(CAST(content AS BLOB)) BETWEEN 1 AND 1048576),
		status TEXT NOT NULL CHECK (status IN ('present', 'removed_before_dispatch', 'removed_delivery_unknown', 'removed_after_delivery')),
		removed_at TEXT CHECK (length(CAST(removed_at AS BLOB)) BETWEEN 1 AND 64),
		CHECK ((status = 'present' AND content IS NOT NULL AND removed_at IS NULL)
			OR (status <> 'present' AND content IS NULL AND removed_at IS NOT NULL))
	)`,
	`CREATE TABLE context_manifest_exclusions (
		manifest_id TEXT NOT NULL REFERENCES context_manifests (id),
		ordinal INTEGER NOT NULL CHECK (typeof(ordinal) = 'integer' AND ordinal >= 0),
		record_id TEXT CHECK (length(CAST(record_id AS BLOB)) BETWEEN 1 AND 256),
		version_id TEXT CHECK (length(CAST(version_id AS BLOB)) BETWEEN 1 AND 256),
		identity_withheld INTEGER NOT NULL CHECK (identity_withheld IN (0, 1)),
		mandatory INTEGER NOT NULL CHECK (mandatory IN (0, 1)),
		stage TEXT NOT NULL CHECK (stage IN ('gather', 'eligibility', 'conflict', 'content', 'size', 'redaction', 'render', 'admission')),
		kind TEXT NOT NULL CHECK (length(CAST(kind AS BLOB)) BETWEEN 1 AND 256),
		reason TEXT NOT NULL CHECK (length(CAST(reason AS BLOB)) BETWEEN 1 AND 4096),
		PRIMARY KEY (manifest_id, ordinal),
		CHECK ((identity_withheld = 1 AND record_id IS NULL AND version_id IS NULL)
			OR (identity_withheld = 0 AND record_id IS NOT NULL))
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
