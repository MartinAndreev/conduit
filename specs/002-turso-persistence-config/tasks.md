# Tasks

## 002-A Storage foundation and Turso compatibility

- [ ] Define system storage interfaces for database handles, prepared statements, transactions, batches, shutdown hooks, and sanitized storage errors.
- [ ] Implement embedded Turso adapter using `@tursodatabase/database` for ordinary Node and Bun execution.
- [ ] Implement project database path resolution at `<project>/.conduit/state.db` and Git-ignore verification for DB/WAL/SHM/backup artifacts.
- [ ] Implement user-global database path resolution for Linux, macOS, and Windows app-data directories.
- [ ] Implement project single-instance lock that prevents two Conduit processes from owning the same project database.
- [ ] Add tests proving spawned runner planning/launch context does not include database paths, connection strings, or database helper imports.

## 002-B Migration framework and recovery

- [ ] Define migration metadata contracts with ID, owner domain, scope, checksum, applied timestamp, duration, and status.
- [ ] Implement ordered migration registry and in-process migration lock.
- [ ] Create initial system migration tables for project and global database scopes.
- [ ] Add pre-migration backup creation and recovery metadata.
- [ ] Add checksum mismatch, interrupted migration, rollback/restore, and corruption-reporting tests.

## 002-C Global profiles and configuration precedence

- [ ] Add global profile schema and repository contracts under the configuration domain.
- [ ] Persist reusable role defaults: runner, model, effort, mode, read-only, ownership defaults, skill source defaults, and metadata.
- [ ] Update settings resolution to merge built-in defaults, user-global profile, `conduit.yml`, and project role guidance in the approved order.
- [ ] Expose field provenance for resolved settings.
- [ ] Add tests for partial overrides and for secrets being rejected/redacted from persisted profile fields.

## 002-D Project-state repositories and legacy import

- [ ] Add Turso-backed refinement draft repository with optimistic versioning.
- [ ] Add Turso-backed refinement revision, architect event, research event/report, question/answer, and approval-state repositories.
- [ ] Add Turso-backed run event, review result, run snapshot, cancellation, and recovery metadata repositories.
- [ ] Add source-version metadata primitives required by Feature 003 without implementing memory retrieval.
- [ ] Add idempotent import ledger and importers for existing `.conduit` JSON/file state.
- [ ] Preserve feature packet files as approved Git-visible artifacts only; do not write drafts or run history to `specs/`.

## 002-E Security, shutdown, and corruption behavior

- [ ] Add shared redaction service integration before all text-bearing database writes and diagnostics.
- [ ] Add seeded-secret tests for project DB, global DB, migration backups, imported records, logs, prompts, and snapshots.
- [ ] Add clean shutdown that flushes queued writes, finalizes statements, closes handles, releases locks, and checkpoints where supported.
- [ ] Add bounded transaction and batch-size tests.
- [ ] Add actionable corruption/error reporting tests with sanitized messages.

## 002-F Standalone native-binding packaging gate

- [ ] Design and document a maintainable target-specific Turso native-binding adapter or Bun build plugin/alias.
- [ ] Update standalone build pipeline for Linux x64 glibc, Linux ARM64 glibc, macOS ARM64, and Windows x64.
- [ ] Reject unsupported Alpine/musl and Intel macOS builds with clear errors until validated.
- [ ] Add standalone executable tests that run without `node_modules` and create/open both databases.
- [ ] Verify migrations, persistence across reopen, prepared statements, transactions, batched writes, interrupted migration recovery, correct native binding packaging, and secret absence in standalone mode.
