# Harness storage slice

## Contract

This group adds a fresh, local Turso Database store for logical sessions and a
durable per-run event journal. Domain and application packages own repository
and unit-of-work ports. Only `internal/storage/turso` can access `database/sql`,
the Turso driver, SQL text, connections, or transactions.

Session writes use optimistic versions. Journal event IDs are idempotent only
when all supplied content is identical; reuse for different content fails.
Journal sequence is monotonic per run, assigned in the same transaction as the
append, and replay is ordered with an exclusive cursor.

The adapter creates only a new Go-owned database. A non-empty database without
the exact Conduit schema marker is rejected before any schema mutation. Legacy
import, remote sync, credentials, global configuration, orchestration, and cloud
access are outside this group.

Bootstrap assumes the process-level single-writer ownership required by the Go
architecture. Coordinating simultaneous first opens is outside this slice.

## Task

Implement one embedded Turso adapter with local mode, repository adapters, a
consumer-owned atomic unit of work, and Jet's parameterized SQLite query builder.
Repositories use generated table types. The current development-only generator
rebuilds them from the initial bootstrap schema using a fresh temporary Turso
database; this is a bounded bootstrap mechanism, not the future migration
authority. Keep the boundary suitable for a future mode supported by the same
Turso SDK without adding that mode now.

Jet v2.15.0 was verified against Turso Go v0.7.2: generated parameterized insert,
select, and update statements executed through Turso; transaction rollback held;
and `GenerateDB` produced compiling table/model code from that Turso connection.

## Migration and generation proposal (not implemented)

Replace the initial bootstrap gate with ordered, versioned migration definitions.
Those definitions become the only schema authority. A runner should acquire the
single-process owner lock, open a transaction, verify application identity plus
the applied version/checksum ledger, apply each pending migration exactly once,
record its checksum and version in that transaction, and fail closed on an
unknown version or changed checksum. Startup must never infer compatibility from
Jet-generated code.

Jet regeneration should create a fresh temporary Turso database, run the same
versioned migrations through the production runner, call `GenerateDB` on that
Turso `*sql.DB`, and replace only a dedicated generated-code directory. It must
never open a user database or use `GenerateDSN`, whose SQLite generator path
selects a separate `sqlite3` driver. Keep generator dependencies in development
tooling so `go-sqlite3` and CGO do not enter the runtime build.

Migration library selection remains open. The accepted runner needs explicit
support for an existing `*sql.DB` or a Turso driver name, transactions controlled
by Conduit, embedded migration sources, checksums, and no assumption that the
database is opened through a SQLite-specific driver. Import or compatibility with
the TypeScript database remains a separate deferred feature; these initial tables
do not claim to port any legacy migration.

The future runner must preserve the useful guarantees of the TypeScript system:
migrations belong to an explicit domain and database scope; checksum input is
stable and verified; history records distinguish running, succeeded, and failed
attempts; and pending work is preceded by a validated backup and checkpoint with
recovery metadata. The Go design should improve one known gap: schema changes and
the succeeded history record must commit atomically rather than committing DDL
and recording completion afterward. Failure diagnostics must remain available
without treating a failed migration as applied.

These are schema migration obligations for Go-owned databases. Importing rows or
reconciling schema/history from an existing TypeScript database is a different,
explicitly deferred data-import and cutover workflow with its own compatibility,
backup, validation, and rollback contract.

## Checks

Run `go test ./...`, `go vet ./...`, `go build ./...`, and
`go run ./cmd/conduit --help`. Storage tests use temporary databases and cover
schema creation/reopen, incompatible-file rejection without mutation, session
CAS, parameter binding, journal ordering/cursors/deduplication/conflicts, and
unit-of-work commit and rollback.
