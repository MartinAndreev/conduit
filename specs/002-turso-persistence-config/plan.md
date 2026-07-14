# Implementation plan

## Dependency position

Feature 002 must be completed before Feature 003 implementation begins. It supplies database ownership, migrations, configuration resolution, repository infrastructure, and standalone packaging gates. Feature 003 may add memory/indexing migrations through the 002 migration registry, but must not duplicate connection bootstrap or migration infrastructure.

## Domain ownership

- `system`: embedded Turso adapter, connection factories, migration runner, transaction/batch helpers, locks, shutdown, storage errors, release native-binding adapter.
- `configuration`: global profile schema and configuration-resolution query behavior.
- `refinement`: draft, revision, research, architect-event, clarification, and approval-state repositories.
- `runs`: run-event, review-result, run-snapshot/recovery, process-state metadata repositories.
- `features`: committed packet repository remains Git/file-backed; optional read cache may live in the project DB but approved packet files remain the source of truth.
- `credentials`: remains OS vault/encrypted fallback; no credential payload tables are introduced.

## Task groups

### Group 002-A: Storage foundation and Turso compatibility

Deliverables:

- Add system storage contracts and an embedded Turso implementation using `@tursodatabase/database`.
- Add project/global database factories and path resolution.
- Add project single-instance locking independent of experimental Turso multi-process WAL.
- Add prepared-statement, bounded-transaction, batched-write, shutdown, and sanitized-error primitives.
- Add ordinary Node and ordinary Bun smoke tests.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- persistence-foundation`
- `pnpm --filter conduit-orchestrator typecheck`

### Group 002-B: Migration framework and recovery

Deliverables:

- Add ordered migration registry by database scope and domain owner.
- Add checksummed migration history tables.
- Add in-process migration locking.
- Add pre-migration backups and interrupted-migration recovery metadata.
- Add checksum mismatch, corruption, rollback/restore, and redacted diagnostics tests.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- migrations`
- `pnpm --filter conduit-orchestrator test -- migration-recovery`

### Group 002-C: Global profiles and configuration precedence

Deliverables:

- Add global profile schema in the user-global database.
- Add global profile repository interfaces/types in the configuration domain.
- Update settings resolution to merge built-in defaults, global profile, `conduit.yml`, and project role guidance with field provenance.
- Ensure role skill guidance remains advisory and cannot smuggle credentials into persisted configuration.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- configuration`
- `pnpm --filter conduit-orchestrator test -- global-profile`

### Group 002-D: Project-state repositories and legacy import

Deliverables:

- Replace file-backed intermediate repositories with Turso-backed repositories for refinement and run domains.
- Preserve committed feature packet behavior: approved packet files remain in `specs/`; drafts and runtime artifacts remain in `.conduit/state.db`.
- Import existing `.conduit` JSON/file state idempotently with an import ledger.
- Add optimistic versioning for mutable drafts/revisions and deterministic sequence fields for append-only events.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- refinement`
- `pnpm --filter conduit-orchestrator test -- runs`
- `pnpm --filter conduit-orchestrator test -- legacy-import`

### Group 002-E: Security, shutdown, and corruption behavior

Deliverables:

- Add redaction gate for all persisted text-bearing records and migration/import diagnostics.
- Add seeded-secret tests across project DB, global DB, backups, logs, run state, prompts, and import outputs.
- Add clean shutdown/checkpoint behavior and interrupted-run recovery behavior.
- Add corruption/error reporting with actionable remediation.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- persistence-security`
- `pnpm --filter conduit-orchestrator test -- shutdown-recovery`

### Group 002-F: Standalone native-binding packaging gate

Deliverables:

- Add a maintainable target-specific Turso native-binding adapter or Bun build plugin/alias.
- Update standalone build targets to produce supported artifacts for Linux x64 glibc, Linux ARM64 glibc, macOS ARM64, and Windows x64.
- Mark Alpine/musl and Intel macOS unsupported until validated.
- Add standalone tests that run without `node_modules` and exercise both databases, migrations, persistence, prepared statements, transactions, batched writes, recovery, binding identification, unsupported-platform errors, and secret absence.

Completion checks:

- `pnpm --filter conduit-orchestrator build:standalone -- linux-x64`
- `pnpm --filter conduit-orchestrator test -- standalone-persistence`
- Equivalent CI/release jobs for each supported target before release publication.

## Migration order

1. `0001_system_project_metadata` in project DB.
2. `0002_configuration_global_profiles` in global DB.
3. `0003_refinement_state` in project DB.
4. `0004_runs_state` in project DB.
5. `0005_feature_packet_cache_optional` in project DB, only if read caching is implemented.
6. `0006_source_versions_primitives` in project DB for Feature 003 evidence tracking.
7. Feature 003 migrations start after these IDs and use the same registry.

## Failure and recovery behavior

- Startup fails before opening runners if the project lock is held by another active Conduit process.
- Migration checksum mismatch fails startup and prints the offending migration ID, database scope, and remediation guidance without dumping row contents.
- Interrupted migrations are detected from migration history/recovery metadata and handled by rollback, retry, or backup restore depending on transaction state.
- Corruption reports include the database scope and backup location but no secrets.
- Import failures skip the invalid legacy item, record a diagnostic, and continue importing other valid items.

## Verification strategy

- Unit tests for path resolution, config precedence, redaction, migration ordering/checksums, optimistic writes, batching, and error mapping.
- Integration tests using temporary project/global directories for import and repository behavior.
- Process tests for single-instance lock and no database handoff to agents.
- Standalone executable tests in release CI for supported targets.
- Markdown/spec checks for this packet before implementation begins.
