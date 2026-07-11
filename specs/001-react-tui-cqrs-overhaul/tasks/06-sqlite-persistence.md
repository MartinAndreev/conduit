# Group 6 — SQLite state persistence and recovery

## Scope

Replace scattered mutable local state with SQLite while retaining Markdown feature packets as the committed source of truth.

## Tasks

- [ ] Introduce versioned SQLite schemas and migrations for global and per-project state.
- [ ] Validate a Kysely and SQLite-driver combination in both the Node package and Bun standalone binary before committing to the production driver.
- [ ] Store global preferences, recent projects, and credential-profile references in the platform data directory.
- [ ] Store project drafts, lifecycle metadata, runs, normalized events, transcript indexes, changed-file snapshots, and review results in ignored `.conduit/state.db`.
- [ ] Implement domain-owned repositories that receive a database executor through their constructor; handlers receive repository instances through dependency injection, and no TUI or helper code accesses SQLite directly.
- [ ] Enable WAL mode and append-only event writes; provide replay/recovery queries for interrupted runs.
- [ ] Migrate existing JSON run state safely and provide an explicit backup/rollback path.
- [ ] Keep credential material in the OS keychain where available; persist only references or encrypted fallback ciphertext.
- [ ] Add migration, concurrency, recovery, and secret-redaction tests.

## Acceptance criteria

- [ ] A run interrupted during output capture can be reopened with its ordered event timeline and changed-file state.
- [ ] Existing project configuration and run history migrate without data loss.
- [ ] No plaintext credential appears in either database, run event, transcript, or snapshot.
- [ ] Markdown specs remain readable and reviewable without SQLite.
