# Conduit

Conduit is local-first orchestration for coding agents. It helps turn a feature
idea into an approved packet, coordinate bounded specialist work, and review
the result.

For installation and usage, see the repository documentation:
https://github.com/MartinAndreev/conduit

Interactive Home performs a non-blocking check of the official GitHub Releases
feed and offers a default-cancel `u` update flow when a newer stable version is
available. Supported official Unix standalone builds use staged SHA-256-verified
atomic replacement; positively detected global npm, pnpm, and Bun installs use
a shell-free exact-version update. Unsupported, local, source, read-only, and
Windows standalone installs remain manual. See the repository README for the
complete integrity, offline, restart, and fallback behavior.

Developer and agent documentation for the embedded Turso/Kysely persistence
API is available in [docs/database-api.md](docs/database-api.md).

## Standalone storage builds

Standalone releases replace Turso's dynamic native loader at build time with
one statically referenced binding package. The target table and build plugin
live in `scripts/build-standalone.js`; adding a target requires a validated Bun
target, matching `@tursodatabase/database-*` optional dependency, release CI
runner, and the standalone persistence suite. Linux musl and Intel macOS are
intentionally rejected until their native bindings pass that suite.

Domain migrations and repositories use Kysely. Conduit applies each Kysely
schema migration in a Kysely transaction through its own runner because the
feature contract also requires scoped checksums, pre-migration backups,
interruption records, and recovery guidance. The stock Kysely `Migrator` is not
used with embedded Turso: its migration flow leaves the current Turso binding's
WAL checkpoint busy, while direct Kysely transactions persist and reopen
cleanly. Raw SQL is limited to the dialect/driver boundary (`BEGIN`, `COMMIT`,
`ROLLBACK`), WAL checkpointing, and Turso's `VACUUM INTO` backup primitive.
