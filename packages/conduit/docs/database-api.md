# Conduit database API

This guide is the developer contract for Conduit's embedded database layer. It
is intended for maintainers and coding agents adding migrations, repositories,
commands, queries, or startup behavior.

The API is currently public **inside the Conduit source tree**. The npm package
publishes the CLI and does not expose a JavaScript library `exports` map, so
downstream packages must not import these modules as a versioned npm API yet.
Within this repository, treat the interfaces and extension patterns described
here as stable boundaries.

## Mental model

Conduit uses two embedded Turso databases:

| Scope   | Default location                                           | Owns                                                                                                                 |
| ------- | ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Project | `<project>/.conduit/state.db`                              | Drafts, refinement state, run events, reviews, run snapshots/recovery, import history, and source-version primitives |
| Global  | Platform application-data directory as `conduit/global.db` | Reusable global role profiles                                                                                        |

The project state directory can be changed by `stateDir` in `conduit.yml`.
Global paths follow the operating system:

- Linux: `${XDG_DATA_HOME:-~/.local/share}/conduit/global.db`
- macOS: `~/Library/Application Support/conduit/global.db`
- Windows: `${APPDATA}/conduit/global.db`

The database stack has four layers:

```text
command/query handler
        │
domain repository interface and Turso implementation
        │
Kysely query builder and domain-owned schema type
        │
DatabaseConnection → embedded Turso native binding
```

Business code should normally use a domain repository. Direct Kysely access is
for repository implementations and migrations. Direct `DatabaseConnection`
access is infrastructure-level work.

## Startup is a hard boundary

Application startup runs storage preparation before application repositories or
query buses are opened:

1. Show the blocking migration screen when running interactively.
2. Resolve project configuration while the screen is active.
3. Migrate the global database.
4. Migrate the project database.
5. Import legacy file-backed state.
6. Open application repositories and allow queries.

The coordinator is `DefaultStartupMigrationRunner`. The CLI wraps it with
`runMigrationScreen`, which remains visible on failure until the user dismisses
it. Non-interactive processes run the same stages without rendering the TUI.

Do not move a database query ahead of this sequence. A new startup consumer must
enter through the existing startup coordinator, not open a repository while the
migration screen is still being constructed.

## Opening and closing databases

### Project database

Use `ProjectDatabaseFactory` when code owns a project connection:

```ts
import { ProjectDatabaseFactory } from "@system/storage/factories/database-factories.js";
import { DefaultDatabaseLifecycle } from "@system/storage/repositories/database-lifecycle.js";

const factory = new ProjectDatabaseFactory(
  projectRoot,
  undefined,
  config.stateDir,
);
const connection = await factory.open();

const lifecycle = new DefaultDatabaseLifecycle();
lifecycle.registerConnection(connection);

try {
  // Construct repositories that share this connection.
} finally {
  await lifecycle.shutdown();
}
```

`open()` performs all ownership work:

- resolves and creates the state directory;
- verifies that database, WAL, SHM, and backup artifacts are Git-ignored;
- acquires the project single-owner lock;
- opens embedded Turso;
- applies project migrations idempotently; and
- restricts database permissions where the operating system supports it.

Closing the returned connection checkpoints WAL state, finalizes statements,
closes Turso, and releases the project lock. Do not open a separate project
connection per repository: the second connection will correctly fail the
single-owner check. Open one connection at the application boundary and share
it among project repositories.

The normal application does exactly this in `createBootstrapComposition`. It
creates one application-scoped `LazyDatabaseConnection` and injects that same
object into every project repository. Lazy opening avoids database work for an
application that never uses project persistence; it does not create a
connection per repository or per query. The migration screen has already
completed before the lazy connection is allowed to open. CLI one-shot commands
use `withProjectStorage`, which eagerly opens one connection for the command,
shares it, and closes it once.

### Global database

Use `GlobalDatabaseFactory` for global storage:

```ts
const factory = new GlobalDatabaseFactory(process.env);
const connection = await factory.open();
try {
  // Global database work.
} finally {
  await connection.close();
}
```

`openWithoutMigrations()` exists for `DefaultStartupMigrationRunner`, which must
render progress around migration execution. Ordinary application code should
use `open()`.

### Lifecycle API

`DatabaseLifecycle` owns orderly shutdown:

```ts
interface DatabaseLifecycle {
  registerStatement(statement: DatabaseStatement): void;
  registerConnection(connection: DatabaseConnection): void;
  registerHook(hook: ShutdownHook): void;
  shutdown(): Promise<readonly string[]>;
}
```

Shutdown is idempotent. It finalizes statements, checkpoints and closes
connections, then closes other hooks. Failures do not prevent later resources
from closing; returned and emitted diagnostics are secret-redacted.

## Bootstrap contract

`createApplication` is intentionally a small composition root. It creates the
command and query buses, builds shared repositories and lifecycle ownership,
then invokes a list of `ApplicationBootstrapService` registrars.

```ts
export interface ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void;
}

export interface ApplicationBootstrapContext {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
  readonly dependencies: BootstrapDependencies;
  readonly projectRoot?: string;
  readonly repositories: BootstrapRepositories;
  readonly processRegistry: RunProcessRegistry;
}
```

Default registration is split by responsibility:

- `CoreBootstrapService` owns project initialization/bootstrap state;
- `ConfigurationBootstrapService` owns settings and credentials;
- `FeaturesBootstrapService` owns feature commands and queries;
- `RolesBootstrapService` owns role/portrait queries;
- `RefinementBootstrapService` owns drafts and refinement workflows; and
- `RunsBootstrapService` owns run execution, events, reviews, and recovery.

Repository and lifecycle construction lives separately in
`createBootstrapComposition`. A domain bootstrap service registers handlers; it
does not construct a second database connection or own shutdown.

New domains should implement the contract rather than add registrations to
`application.ts`:

```ts
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "@system/bootstrap/application.js";

export class WidgetsBootstrapService implements ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void {
    context.queryBus.register(
      "listWidgets",
      createListWidgetsHandler(context.repositories.widgets),
    );
  }
}
```

When a new repository is shared through the context, add its domain interface
to `BootstrapRepositories`, construct the Turso implementation in
`createBootstrapComposition`, and register the new service in
`createDefaultBootstrapServices`. Keep optional capabilities guarded when the
application can run without a project database.

Tests may supply a focused registrar list through the second argument:

```ts
const application = createApplication(dependencies, [
  new WidgetsBootstrapService(),
]);
```

This is useful for contract tests; production uses the default registrar list.

## Preferred repository API

Handlers depend on domain interfaces, not Turso, Kysely, table names, or JSON
files. Composition code chooses the implementation:

```ts
import type { RunRecoveryRepository } from "@domains/runs/interfaces/run-recovery-repository.js";
import { TursoRunRecoveryRepository } from "@domains/runs/repositories/turso-run-recovery-repository.js";

const recovery: RunRecoveryRepository = new TursoRunRecoveryRepository(
  connection,
);

const initial = await recovery.saveSnapshot(run);
run.status = "completed";
await recovery.saveSnapshot(run, initial.version);
```

Passing `expectedVersion` makes a mutable update optimistic. A stale writer is
rejected rather than silently overwriting a newer snapshot. Drafts, revisions,
profiles, and other mutable records follow the same rule where their interface
exposes a version.

State-changing calls belong behind command handlers. Reads belong behind query
handlers. TUI components receive read models and dispatch commands; they never
open databases directly.

### Repository catalog

| Domain        | Interface                      | Turso implementation                |
| ------------- | ------------------------------ | ----------------------------------- |
| Configuration | `GlobalProfileRepository`      | `TursoGlobalProfileRepository`      |
| Refinement    | `DraftRepository`              | `TursoDraftRepository`              |
| Refinement    | `RefinementRevisionRepository` | `TursoRefinementRevisionRepository` |
| Refinement    | `ArchitectEventRepository`     | `TursoArchitectEventRepository`     |
| Refinement    | `ResearchReportRepository`     | `TursoResearchReportRepository`     |
| Runs          | `RunEventRepository`           | `TursoRunEventRepository`           |
| Runs          | `ReviewResultRepository`       | `TursoReviewResultRepository`       |
| Runs          | `RunRecoveryRepository`        | `TursoRunRecoveryRepository`        |
| Source        | `SourceVersionRepository`      | `TursoSourceVersionRepository`      |

The old file repository implementations remain useful as compatibility or test
fixtures, but production composition uses Turso. `run.json` is legacy import
input only; it is not live application state.

## Writing a new repository

Every table and repository belongs to a domain. Do not add a global schema or
repository directory.

### 1. Define the database shape in the domain

```ts
// src/domains/widgets/interfaces/database-schema.ts
export interface WidgetsTable {
  widget_id: string;
  label: string;
  version: number;
  updated_at: string;
}

export interface WidgetsDatabase {
  widgets: WidgetsTable;
}
```

These types describe database rows. Public business values belong in the
domain's `types/` directory, and the repository contract belongs in the
domain's `interfaces/` directory.

### 2. Implement the repository with Kysely

```ts
import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";
import type { WidgetsDatabase } from "../interfaces/database-schema.js";
import type { WidgetRepository } from "../interfaces/widget-repository.js";

export class TursoWidgetRepository implements WidgetRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<WidgetsDatabase>(connection);
  }

  async save(input: Widget): Promise<void> {
    const widget = redactPersistedValue(input);
    await this.database
      .insertInto("widgets")
      .values({
        widget_id: widget.id,
        label: widget.label,
        version: widget.version,
        updated_at: widget.updatedAt,
      })
      .onConflict((conflict) =>
        conflict.column("widget_id").doUpdateSet({
          label: widget.label,
          version: widget.version,
          updated_at: widget.updatedAt,
        }),
      )
      .execute();
  }
}
```

Use Kysely for selects, inserts, updates, deletes, joins, schema builders, and
transactions. Parameterization and statement finalization are handled by the
Conduit Kysely dialect.

The Kysely instance does not own the underlying Conduit connection. Destroying
it does not release the project lock; the application boundary must close the
`DatabaseConnection` or its `DatabaseLifecycle`.

### 3. Wire handlers through the interface

Instantiate the Turso repository in system composition, then pass its interface
to command/query handler factories. Do not import the Turso implementation into
screens or business types.

## Transactions and batches

Use a Kysely transaction for related repository writes:

```ts
await this.database.transaction().execute(async (transaction) => {
  await transaction.insertInto("widgets").values(widgetRow).execute();
  await transaction.insertInto("widget_events").values(eventRow).execute();
});
```

Transactions use `BEGIN IMMEDIATE` and must be short. Never keep a transaction
open while an agent, subprocess, network call, user prompt, or TUI interaction
is running.

`BoundedBatchWriter<T>` is available when a caller supplies a single-item write
operation and needs an explicit maximum batch size:

```ts
const writer = new BoundedBatchWriter<Widget>(
  (widget) => repository.save(widget),
  100,
);
await writer.writeBatch(widgets);
```

The writer rejects oversized input before beginning work. Choose a bound that
keeps transactions and shutdown latency predictable.

`DatabaseTransactionRunner` is the low-level transaction primitive for code
that cannot use Kysely. Domain repositories should prefer Kysely transactions.

## Low-level connection API

Infrastructure code may use the provider-neutral contracts:

```ts
interface DatabaseConnection {
  readonly databasePath: string;
  execute(sql: string, parameters?: SqlParameters): Promise<QueryResult>;
  prepare(sql: string): Promise<DatabaseStatement>;
  backup(destinationPath: string): Promise<void>;
  checkpoint(): Promise<void>;
  close(): Promise<void>;
}
```

Prepared statements must always be finalized:

```ts
const statement = await connection.prepare(
  "SELECT id FROM schema_migrations WHERE scope = ?",
);
try {
  const result = await statement.all(["project"]);
  // result.rows is readonly data; result.rowsAffected covers writes.
} finally {
  await statement.finalize();
}
```

Do not expose a connection, statement, database path, Kysely instance, or
database environment variable to a spawned agent. Runner processes are created
with database-related environment keys removed.

## Migrations

Migrations are domain-owned Kysely schema operations registered in the central
default registry.

```ts
import type { MigrationDefinition } from "@system/storage/types/migration.js";

export const widgetsMigration = {
  id: "0007_widgets",
  domain: "widgets",
  scope: "project",
  checksumSource: "widgets-v1",
  async up(database) {
    await database.schema
      .createTable("widgets")
      .ifNotExists()
      .addColumn("widget_id", "text", (column) => column.primaryKey())
      .addColumn("label", "text", (column) => column.notNull())
      .addColumn("version", "integer", (column) =>
        column.notNull().defaultTo(1),
      )
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
  },
} satisfies MigrationDefinition;
```

Then register it in `createDefaultMigrationRegistry()`.

Migration rules:

- IDs are stable, ordered within their database scope, and never reused.
- `domain` identifies the schema owner.
- `scope` is `project` or `global`.
- `checksumSource` is explicit and stable across source, bundled, Node, Bun,
  and standalone execution.
- Never edit an applied migration. Add a new migration.
- Use SQLite-compatible Kysely schema features.
- Keep migrations deterministic and free of provider credentials.

The custom migration runner deliberately wraps Kysely schema builders instead
of Kysely's stock `Migrator`. It adds Conduit-specific scope, domain ownership,
checksums, durations, in-process serialization, backups, recovery records, and
sanitized remediation. Direct Kysely transactions also reopen cleanly with the
embedded Turso binding, whereas the stock migration flow left WAL checkpoint
state busy during compatibility testing.

Before pending migrations run, Conduit checkpoints the database, creates a
timestamped backup, and scans it for known or environment-derived secrets. A
failed scan removes the backup and aborts migration. Each migration runs in its
own transaction. A `running` history record on the next startup is treated as
interrupted and retried safely.

Raw SQL is restricted to the infrastructure boundary:

- `BEGIN IMMEDIATE`, `COMMIT`, and `ROLLBACK` in the Kysely driver and low-level
  transaction helper;
- `PRAGMA wal_checkpoint(TRUNCATE)` for shutdown durability; and
- `VACUUM INTO` for embedded Turso backups.

Do not add raw domain queries when Kysely can express the operation.

## Legacy file migration

`LegacyFileImporter` imports existing local state after project migrations:

- `.conduit/drafts/*.json`;
- run events, reviews, and `run.json` snapshots under `.conduit/runs/`;
- legacy `.log`, `.patch`, and `.diff` run artifacts;
- feature research reports; and
- refinement revisions, questions, answers, and architect transcripts.

Each source path and content checksum is recorded in `import_ledger`. An
unchanged successful import is skipped. Changed input is imported again.
Invalid input records a redacted failure and remains on disk. Import never
deletes legacy files and never copies intermediate state into `specs/`.

When adding a legacy importer:

1. run it only after its destination schema exists;
2. pass all text through `redactSecrets` or `redactPersistedValue`;
3. use the import ledger for idempotency;
4. keep the original file; and
5. test invalid input and repeated startup.

## Security contract

Database storage is not a credential store. Provider credentials stay in the
OS vault or encrypted fallback store.

Before persisting text or structured values:

```ts
const safeText = redactSecrets(input);
const safeObject = redactPersistedValue(input);
```

`redactSecrets` removes recognized API keys, tokens, passwords, bearer values,
private keys, and values of secret-named environment variables. Structured
redaction also replaces values under secret-like property names.

Do not persist or log raw caught errors. Convert infrastructure errors with
`toStorageError` or sanitize them with `redactStorageDiagnostic`. A
`StorageError` exposes the database scope, failed operation, and remediation
without leaking the original secret-bearing error.

## Errors and recovery

Expected operational behavior:

- A second project owner fails before opening the database and receives lock
  ownership guidance.
- A checksum mismatch is a hard startup failure; restore the original migration
  or a known-good backup.
- A corrupt database stops startup and reports scoped, redacted recovery
  guidance.
- A stale optimistic update fails instead of overwriting newer state.
- Abrupt runs are marked through `RunRecoveryRepository.markInterrupted()`;
  cancellations use `markCancelled()`.
- Shutdown continues closing remaining resources even if one close operation
  fails.

The diagnostic command exercises the installed runtime without an agent call:

```sh
conduit storage-doctor --project /path/to/project
```

It verifies both databases, migrations, prepared queries, a transaction,
bounded batch writing, close/reopen persistence, interrupted migration recovery,
and the active native binding.

## Standalone runtime

Ordinary Node and Bun resolve `@tursodatabase/database` dynamically. Standalone
Bun builds cannot rely on that dynamic N-API loader, so
`scripts/build-standalone.js` selects one statically referenced native package
from its target map.

Supported release targets are:

- Linux x64 glibc;
- Linux ARM64 glibc;
- macOS ARM64; and
- Windows x64.

Linux musl and Intel macOS are rejected until their bindings pass the complete
standalone persistence suite. Adding a target requires a native optional
dependency, target-map entry, CI runner, and the same persistence tests.

`@tursodatabase/database` and `kysely` are direct runtime dependencies because
storage is required application behavior. Target-native
`@tursodatabase/database-*` packages are optional dependencies selected by the
standalone target map; application code never imports them directly.

## Testing a database change

Repository tests should use a temporary project and the real embedded adapter:

```ts
const projectRoot = await mkdtemp(join(tmpdir(), "conduit-widget-"));
try {
  const factory = new ProjectDatabaseFactory(projectRoot);
  const connection = await factory.open();
  const repository = new TursoWidgetRepository(connection);
  // Exercise writes, reads, redaction, ordering, and stale versions.
  await connection.close();
} finally {
  await rm(projectRoot, { recursive: true, force: true });
}
```

At minimum, cover:

- fresh migration and reopen persistence;
- idempotent migration or import behavior;
- optimistic conflicts for mutable state;
- deterministic sequence order for append-only state;
- secret absence in database and backup bytes for text-bearing storage;
- corruption or interrupted-operation behavior where relevant; and
- lifecycle cleanup so the project lock can be reacquired.

Before handoff, run:

```sh
pnpm --filter conduit-orchestrator lint
pnpm --filter conduit-orchestrator typecheck
pnpm --filter conduit-orchestrator test
pnpm --filter conduit-orchestrator build
pnpm --filter conduit-orchestrator build:standalone -- linux-x64
pnpm --filter conduit-orchestrator start --help
```

## Contributor checklist

- Put schema types, migrations, repository contracts, and implementations in
  their owning domain.
- Use `.js` specifiers in relative TypeScript imports.
- Use Kysely for domain queries and migrations.
- Redact before every text-bearing database write.
- Keep transactions short and bounded.
- Share one owned project connection across repositories.
- Persist state through command handlers and read through query handlers.
- Add handler registrations through an `ApplicationBootstrapService`.
- Register new migrations centrally without editing applied migrations.
- Keep database context out of prompts and spawned process environments.
- Close through `DatabaseLifecycle` or an explicit `finally` block.
- Never restore live JSON files as an application-state fallback.
