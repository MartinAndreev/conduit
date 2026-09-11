# Tasks

Implementation checkboxes below reflect the code delivered by `ea0d012`.
Release-matrix verification remains separate from implementation.

## 002-A Storage foundation and Turso compatibility

- [x] Define system storage interfaces for database handles, prepared statements, transactions, batches, shutdown hooks, and sanitized storage errors.
- [x] Implement embedded Turso adapter using `@tursodatabase/database` for ordinary Node and Bun execution.
- [x] Implement project database path resolution at `<project>/.conduit/state.db` and Git-ignore verification for DB/WAL/SHM/backup artifacts.
- [x] Implement user-global database path resolution for Linux, macOS, and Windows app-data directories.
- [x] Implement project single-instance lock that prevents two Conduit processes from owning the same project database.
- [x] Add tests proving spawned runner planning/launch context does not include database paths, connection strings, or database helper imports.

## 002-B Migration framework and recovery

- [x] Define migration metadata contracts with ID, owner domain, scope, checksum, applied timestamp, duration, and status.
- [x] Implement ordered migration registry and in-process migration lock.
- [x] Create initial system migration tables for project and global database scopes.
- [x] Add pre-migration backup creation and recovery metadata.
- [x] Add checksum mismatch, interrupted migration, rollback/restore, and corruption-reporting tests.

## 002-C Global profiles and configuration precedence

- [x] Add global profile schema and repository contracts under the configuration domain.
- [x] Persist reusable role defaults: runner, model, effort, mode, read-only, ownership defaults, skill source defaults, and metadata.
- [x] Update settings resolution to merge built-in defaults, user-global profile, `conduit.yml`, and project role guidance in the approved order.
- [x] Expose field provenance for resolved settings.
- [x] Add tests for partial overrides and for secrets being rejected/redacted from persisted profile fields.

## 002-D Project-state repositories and legacy import

- [x] Add Turso-backed refinement draft repository with optimistic versioning.
- [x] Add Turso-backed refinement revision, architect event, research event/report, question/answer, and approval-state repositories.
- [x] Add Turso-backed run event, review result, run snapshot, cancellation, and recovery metadata repositories.
- [x] Add source-version metadata primitives for future evidence tracking without implementing memory retrieval.
- [x] Add idempotent import ledger and importers for existing `.conduit` JSON/file state.
- [x] Preserve feature packet files as approved Git-visible artifacts only; do not write drafts or run history to `specs/`.

## 002-E Security, shutdown, and corruption behavior

- [x] Add shared redaction service integration before all text-bearing database writes and diagnostics.
- [x] Add seeded-secret tests for project DB, global DB, migration backups, imported records, logs, prompts, and snapshots.
- [x] Add clean shutdown that flushes queued writes, finalizes statements, closes handles, releases locks, and checkpoints where supported.
- [x] Add bounded transaction and batch-size tests.
- [x] Add actionable corruption/error reporting tests with sanitized messages.

## 002-F Standalone native-binding packaging gate

- [x] Design and document a maintainable target-specific Turso native-binding adapter or Bun build plugin/alias.
- [x] Update standalone build pipeline for Linux x64 glibc, Linux ARM64 glibc, macOS ARM64, and Windows x64.
- [x] Reject unsupported Alpine/musl and Intel macOS builds with clear errors until validated.
- [x] Add standalone executable tests that run without `node_modules` and create/open both databases.
- [ ] Verify migrations, persistence across reopen, prepared statements, transactions, batched writes, interrupted migration recovery, correct native binding packaging, and secret absence in standalone mode.

The last task remains an open release-matrix gate. This audit built macOS ARM64
only and did not rerun the Linux and Windows artifacts without `node_modules`.
