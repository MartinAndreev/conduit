# Global project catalog storage

`catalog.Store{Path: absoluteCatalogPath}` implements the project application
repository. The path resolver belongs to `internal/projectinfra`; the normal
location is `~/.conduit/catalog.db`. Construction, missing reads, and preference
writes against missing storage do not create files or directories.

This database has its own application marker and schema v1. Session storage,
its schema v1, and its generator are separate. Existing empty, corrupt, foreign,
partial, or future catalogs are refused without repair or migration. Database
symlinks are refused. Supported DDL is compared after normalizing whitespace
around punctuation and unquoting double-quoted plain identifiers, as Turso
stores them, while retaining word boundaries and string literals; primary
keys, uniqueness, CHECK membership, and foreign keys must still match. Reads
retain dangling preference IDs for application-level diagnostics.

A first registration prepares a private same-directory database. Schema and
project insert share a transaction, followed by WAL checkpoint, connection and
database close, and main-file sync. A hard link publishes the complete database
only if `catalog.db` does not exist. A competing publisher reads the winning
completed database and inserts its registration there. Temporary databases and
sidecars are removed only from the operation's private directory. A preexisting
empty database is therefore distinguishable from an unfinished concurrent init.
A filesystem without hard-link support returns an explicit publication error.

Each operation pins one connection and enables/verifies foreign keys on it.
`BEGIN IMMEDIATE` reserves existing-database writes before validation and reads.
Path uniqueness plus insert-on-conflict returns the winning stable project ID.
Canonical path identity is a filesystem adapter responsibility. Registration
and last-opened preferences are separate transactions; TUI and GUI own separate
preference rows. Caller cancellation is checked before commit. COMMIT then uses an uncanceled
context within the native busy timeout; caller cancellation is reported after
its acknowledged outcome. Successful commit/publication is returned as `Persisted`, even
if subsequent cancellation, directory sync, or resource cleanup reports an
error. Commit failure does not claim completion; callers retry through a read.

On Unix the published directory is synced. Windows has no portable directory
sync through `os.File`; main-file sync is performed, but directory durability
through power loss is not promised. Native Windows runtime is unverified.

`Store.Options` selects engine settings for the published catalog. The default
is single-process: while another process has the file open, an operation fails
with `catalog.ErrDatabaseLocked` instead of `ErrCorruptCatalog`.
`Options.MultiprocessWAL` enables Turso's experimental multi-process WAL; the
private first-registration database always uses the default. Paths containing
`?` are refused because tursogo would parse the rest as DSN parameters.
With the option on, processes opening together before `catalog.db-tshm` exists
race on creating it; the loser's open fails with an engine corruption error
about the coordination file. Only that failure, and only with the option on,
is retried (at most 5 attempts, 10-40ms waits). If it persists, the operation
returns `catalog.ErrSharedMemoryInit`, not `ErrCorruptCatalog`.

The focused tests use temporary directories and independent connectors. They
cover concurrent first publication and subsequent writes, stable duplicates,
separate preferences, no-overwrite validation, foreign-key enforcement,
rollback, and completed publication followed by cancellation/sync failure.
The reservation and checkpoint/main-file publication probes run against Turso.

Generate Jet code independently with:

```sh
go generate ./internal/storage/turso/catalog/schema
```

The nested generator module has a dedicated output directory and its own
pinned dependencies. It does not modify session-generated files.

## Unresolved external deletion race

The existing tursogo v0.7.2 connector has no exposed read-only/no-create or
already-open-file option. If another program removes or replaces `catalog.db`
after the adapter inspects it and before the engine opens it, even a read may
create a new empty file or open the replacement. An extra filesystem check
cannot make this atomic. Normal initially missing reads remain effect-free;
this implementation does not promise no-create safety against concurrent
external deletion. Resolving that stronger guarantee requires an explicitly
approved driver/open-mode change. Conduit itself never deletes catalog files.
