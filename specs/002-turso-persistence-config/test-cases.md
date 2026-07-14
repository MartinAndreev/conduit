# Test cases

## Persistence and migration

- [ ] Fresh project creates `<project>/.conduit/state.db`, fresh user creates platform app-data `global.db`, and both run initial migrations.
- [ ] Migration records are ordered, checksummed, scoped, and attributed to domain owners.
- [ ] Re-running startup after successful migration is idempotent.
- [ ] Editing an already-applied migration checksum fails startup with sanitized remediation guidance.
- [ ] Interrupting a migration leaves recovery metadata and the next startup safely rolls back, retries, or restores from backup.
- [ ] Pre-migration backups are created for existing databases and contain no seeded secrets.
- [ ] Corrupt database files stop writes and produce actionable redacted diagnostics.

## Process ownership and concurrency

- [ ] A second Conduit process for the same project fails safely while the first owns the project lock.
- [ ] Internal concurrent writes use short transactions and do not hold locks during agent execution.
- [ ] Mutable stale writes fail optimistic version checks.
- [ ] Append-only run/refinement events retain deterministic sequence ordering under batched writes.
- [ ] Spawned agent prompts/process environments contain no database path, handle, URL, token, or helper entrypoint.

## Configuration

- [ ] Built-in role defaults apply when no global profile or project override exists.
- [ ] A user-global profile overrides only the fields it explicitly sets.
- [ ] `conduit.yml` overrides global profile fields without erasing unrelated global defaults.
- [ ] Project role guidance is loaded last as advisory prompt content and records provenance separately from structured settings.
- [ ] Secret-like values in global profiles or role guidance are rejected or redacted before persistence.

## Legacy import

- [ ] Existing draft JSON files import idempotently into the refinement repository.
- [ ] Existing run events/reviews import idempotently into run repositories.
- [ ] Invalid legacy JSON is reported and skipped without deleting the source file.
- [ ] Intermediate imported material is not written to Git-visible packet files.

## Security

- [ ] Seeded provider tokens, API keys, private keys, passwords, and environment-secret values do not appear in project DB bytes.
- [ ] The same seeded secrets do not appear in global DB bytes.
- [ ] The same seeded secrets do not appear in backups, logs, prompts, snapshots, or import diagnostics.
- [ ] Credential storage remains OS vault/encrypted fallback only; no plaintext credential table exists.

## Standalone

- [ ] Linux x64 glibc standalone starts without `node_modules` and opens both databases.
- [ ] Linux ARM64 glibc, macOS ARM64, and Windows x64 standalone jobs perform the same persistence suite before release.
- [ ] Standalone migrations, prepared statements, transactions, batched writes, close/reopen persistence, and interrupted-migration recovery pass.
- [ ] The packaged executable reports/uses the expected Turso native binding for its target.
- [ ] Alpine/musl and Intel macOS produce clear unsupported-platform errors until validated.
