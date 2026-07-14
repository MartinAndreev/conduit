# Feature 002: Turso persistence and configuration foundation

## Outcome

Conduit uses embedded `@tursodatabase/database` as the only default persistence engine for local runtime state. A single Conduit process owns all database connections, resolves configuration from layered sources, and exposes domain-owned repositories that replace file-backed intermediate state without leaking drafts, credentials, prompts, or run artifacts into committed feature packets.

Feature 002 is the foundation consumed by Feature 003. It owns database bootstrap, migrations, configuration resolution, generic persistence/repository infrastructure, import from existing JSON/file state, single-instance safety, shutdown, and standalone native-binding compatibility. It does **not** define memory retrieval, context-pack ranking, embedding policy, or handoff lifecycle beyond the generic storage primitives required by later features.

## Scope

### In scope

- Embedded Turso via `@tursodatabase/database` for ordinary Node, ordinary Bun, and supported Bun standalone releases.
- Two separate local databases:
  - project database at `<project>/.conduit/state.db` by default;
  - user-global database in the platform application-data directory, for example `$XDG_DATA_HOME/conduit/global.db` on Linux.
- System-owned storage abstractions, connection factories, transaction helpers, statement helpers, repository testing utilities, and lifecycle hooks.
- Domain-owned schemas and ordered, checksummed migrations.
- Configuration precedence: built-in defaults → user-global profile → `conduit.yml` → project role guidance.
- Global profile storage for reusable role/profile defaults and metadata.
- Project runtime state repositories for drafts, refinement revisions, architect/research events, run events, reviews, source-version metadata placeholders, handoffs/context-pack storage primitives consumed by 003, and recovery metadata.
- Import from existing `.conduit` JSON/file state where applicable.
- Secret redaction and plaintext-credential prevention for database writes, backups, logs, and migration/import paths.
- Clean shutdown, checkpoint behavior, bounded transactions, batching, optimistic versioning, corruption/error reporting, and safe migration recovery.
- Release packaging and acceptance tests proving Turso native `.node` binding compatibility in standalone builds.

### Out of scope

- Turso Cloud, remote URLs, synchronization, auth tokens, replication, or remote database configuration.
- Replacing Turso with ordinary SQLite to avoid packaging work.
- Experimental Turso multi-process WAL support.
- Multiple agent processes opening the database.
- Memory retrieval, vector search, context-pack assembly, global memory promotion, or handoff validation policy; those belong to Feature 003.
- Provider credentials storage. Credentials remain in the OS credential vault or the existing encrypted fallback.

## Architectural decisions

### Process and connection ownership

- Normally one Conduit process runs for a project.
- The Conduit process is the sole owner of project and global database connections.
- Spawned coding agents never receive database paths, handles, connection strings, or helper modules that allow direct database access.
- Accidental simultaneous Conduit launches fail safely through an application-level project lock or equivalent single-instance guard. They must not rely on experimental database coordination.
- Internal Conduit operations may be concurrent, but persistence is coordinated inside the owning process using short transactions, batched writes, optimistic version checks, and repository-level retry/reporting where appropriate.

### Persistence scopes

#### Project database

Default location: `<project>/.conduit/state.db`.

Stores project-local runtime state, including:

- schema migration history and recovery metadata;
- drafts, refinement revisions, research events, architect events, questions, answers, and approval state;
- run events, role prompt metadata, reviews, diffs metadata, and cancellation/recovery state;
- repository source versions and index metadata placeholders required by 003;
- project memory, handoff, context-pack, and retrieval table namespaces created by 003 migrations;
- import markers from legacy JSON/file state.

`.conduit/state.db`, WAL/SHM files, backups, caches, runs, worktrees, and imported legacy artifacts remain ignored by Git. Only explicitly approved packet artifacts are written to `specs/`.

#### User-global database

Default location is resolved through platform application-data conventions:

- Linux: `${XDG_DATA_HOME:-~/.local/share}/conduit/global.db`;
- macOS: `~/Library/Application Support/conduit/global.db`;
- Windows: `%APPDATA%\conduit\global.db`.

Stores user-global-on-this-machine data, including:

- reusable role/profile defaults;
- global configuration metadata;
- explicitly promoted reusable memory tables owned by 003;
- promotion provenance and audit metadata.

Global data is not a raw cross-project knowledge pool. Projects do not automatically share project-specific knowledge.

## Storage architecture

### System infrastructure ownership

Implementation must add system-level persistence infrastructure under `packages/conduit/src/system` and keep domain behavior under `packages/conduit/src/domains/<domain>`. The storage layer provides generic capabilities only:

- open/close project and global connections;
- execute prepared statements;
- run bounded transactions;
- run batched writes with limits;
- expose migration execution;
- map Turso errors into Conduit storage errors;
- redact values before logging;
- register shutdown hooks.

Domain repositories own business semantics and table access. Interfaces, command/query contracts, types, enums, and errors must follow the repository's domain-oriented CQRS conventions.

### Database factories

Required factories:

- `ProjectDatabaseFactory`: resolves the project state directory from configuration, ensures `.conduit` exists, verifies Git ignore protection for database artifacts, acquires the project single-instance lock, opens `<stateDir>/state.db`, runs project migrations, and returns an owned connection handle.
- `GlobalDatabaseFactory`: resolves the platform app-data directory, creates it with restrictive permissions where supported, opens `global.db`, and runs global migrations.
- `DatabaseLifecycle`: closes statements/connections, releases locks, checkpoints WAL state where supported, and emits sanitized shutdown diagnostics.

Factories must never load provider credentials from the database and must never write credential values to the database.

### Repository contracts

Feature 002 must introduce or update repositories behind existing domain interfaces so implementation code stops depending on JSON files for intermediate runtime state. Required repository categories:

- configuration/global profile repository;
- refinement draft repository;
- refinement revision repository;
- architect event repository;
- research report/event repository where applicable;
- run event repository;
- review result repository;
- run recovery/snapshot metadata repository;
- source-version metadata repository primitives consumed by 003;
- generic handoff/context-pack persistence primitives owned by 003 migrations but opened through 002 connection infrastructure.

Repositories must preserve CQRS boundaries. State-changing persistence occurs through command handlers; reads occur through query handlers.

## Migration model

### Ownership

Each domain owns its schema fragments and migration definitions. Migrations are registered centrally by database scope and ordered by a stable migration ID, but schema changes remain attributable to their owning domain.

Initial migration order:

1. `system` project metadata: schema version table, migration history, lock/recovery metadata, import ledger.
2. `configuration` global profile metadata in the global database.
3. `refinement` project draft/revision/research/architect tables.
4. `runs` project run-event/review/recovery tables.
5. `features` project feature metadata cache, if needed for read performance without replacing committed packet files.
6. `source` project source-version metadata primitives for 003 evidence tracking.
7. Reserved extension points for 003 memory, handoff, context-pack, retrieval, and promotion migrations.

### Requirements

- Migrations are ordered and checksummed.
- Applied migration records include migration ID, domain owner, checksum, applied timestamp, execution duration, database scope, and success/failure status.
- Checksum mismatch for an already-applied migration is a hard error with remediation guidance.
- Migration execution is locked within the owning process so concurrent internal startup tasks cannot run migrations twice.
- Before applying migrations to an existing database, Conduit creates a pre-migration backup in `.conduit/backups/` or the global app-data backup directory. Backups must be redaction-checked and never include plaintext credentials.
- Interrupted migrations recover safely: Conduit detects incomplete records, rolls back incomplete transactions where possible, restores from backup when required, and reports the recovery path.
- Migrations use SQLite-compatible schema features where practical and avoid Turso Cloud-only syntax.

## Configuration resolution

Configuration resolves in this exact order, where later layers override earlier ones only for explicitly set fields:

1. built-in defaults;
2. user-global profile from the global database;
3. project `conduit.yml`;
4. project role guidance loaded from configured role skill files.

Resolved settings must include provenance per field for observability and debugging. Global profiles may store runner, model, reasoning effort, mode, read-only default, ownership defaults, skill source defaults, and descriptive metadata. Project role guidance is advisory content, not a place for secrets or database configuration.

## File-state import

The implementation must import existing file-backed state when present:

- drafts from `.conduit/drafts/*.json`;
- refinement revisions and architect/research artifacts from current repository locations;
- run events, run snapshots, reviews, transcript metadata, and diff metadata from `.conduit/runs/` where they exist.

Import requirements:

- imports are idempotent and recorded in an import ledger;
- invalid legacy files are reported and skipped without deleting user data;
- imported records are redacted before storage;
- imported intermediate state remains local and is not copied into `specs/` unless the user explicitly approves packet artifacts through the existing feature workflow;
- legacy files are not removed automatically unless a later task explicitly adds an approved cleanup command.

## Transactions, concurrency, and shutdown

- Writes should use short transactions and explicit batch size limits.
- Long-running agent execution must not hold database transactions open.
- Stale writers inside the application must use optimistic version checks where records can be edited or superseded by another internal operation.
- Append-only event tables must preserve deterministic ordering with monotonic sequence fields per run/draft where needed.
- Shutdown must flush queued writes, close prepared statements, close database handles, release the project lock, and checkpoint WAL state where supported.
- Abrupt termination must leave recovery metadata sufficient to resume, mark in-flight operations interrupted, or report actionable corruption.

## Error, corruption, and security behavior

- Storage errors are reported with sanitized database scope, operation, and remediation guidance.
- Corruption detection must prevent further writes until backup/recovery guidance is shown.
- Secret redaction runs before database writes for logs, prompts, imported artifacts, handoffs, and context-pack text.
- Plaintext credentials, provider tokens, environment secrets, OS-vault payloads, and encrypted-fallback master material must never appear as database rows, backups, context packs, handoffs, prompts, snapshots, or logs.
- Tests must seed known secret patterns and assert they do not appear in either database or backup files.

## Standalone native-binding compatibility

Feature 002 must make Turso a release compatibility gate for Bun standalone builds. Spike findings are accepted:

- `@tursodatabase/database` works under ordinary Node execution.
- It works under ordinary Bun execution.
- A normal `bun build --compile` did not include/resolve Turso's dynamic NAPI-RS native binding.
- A target-specific static CommonJS `require` of `@tursodatabase/database-linux-x64-gnu`, combined with the database-common wrapper, produced a working Linux x64 executable.
- The Linux executable grew from roughly 91 MB to roughly 183 MB.

The implementation must provide a maintainable solution that preserves the normal persistence interface, such as a build-time target-specific native-binding adapter, a Bun plugin/alias that replaces Turso's dynamic loader boundary, or an equivalent documented mechanism. Do not hard-code the spike wrapper as the only acceptable design.

Supported standalone targets for this feature:

- Linux x64 glibc;
- Linux ARM64 glibc;
- macOS ARM64;
- Windows x64.

Alpine/musl and Intel macOS are unsupported until explicitly validated. Unsupported platforms must receive a useful error before release artifacts are claimed supported.

Standalone acceptance tests must prove the executable starts without `node_modules`, creates both database types, runs migrations, persists data across close/reopen, executes prepared statements, transactions, and batched writes, recovers from an interrupted migration, packages the correct platform binding, rejects unsupported platforms clearly, and does not store secrets unintentionally.

## Acceptance criteria

- [ ] Project and global databases are created at the approved default locations and can be overridden only through documented local configuration, not through remote URLs or tokens.
- [ ] The Conduit process is the only database owner; spawned agents receive bounded files/prompts only and no database access.
- [ ] A project single-instance guard prevents unsafe simultaneous Conduit database ownership without relying on experimental Turso multi-process WAL.
- [ ] Domain-owned migrations run in the documented order, are checksummed, create pre-migration backups, and recover safely after interruption.
- [ ] Configuration resolution follows built-in defaults → global profile → `conduit.yml` → project role guidance with field provenance.
- [ ] Existing JSON/file-backed intermediate state imports idempotently into domain repositories without writing unapproved artifacts to `specs/`.
- [ ] Repository interfaces replace file-backed intermediate state for refinement, run, review, and recovery flows while preserving CQRS boundaries.
- [ ] Transactions are short and bounded; stale mutable writes use optimistic version checks.
- [ ] Shutdown closes handles, releases locks, flushes writes, and reports checkpoint/recovery status.
- [ ] Corruption and migration errors produce actionable, redacted diagnostics.
- [ ] Plaintext credentials and seeded secrets are absent from project DB, global DB, backups, logs, imported records, and emitted diagnostics.
- [ ] Bun standalone builds for supported targets package the correct Turso native binding and pass the standalone persistence suite without `node_modules`.
