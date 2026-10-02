//go:generate sh -c "cd ../internal/generate && go run . -output ../generated"

package schema

const Version = "1"
const ApplicationID = "github.com/MartinAndreev/conduit"

var Statements = []string{
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
