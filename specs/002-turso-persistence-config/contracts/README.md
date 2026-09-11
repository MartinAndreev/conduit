# Contracts

Feature 002 contracts delivered by `ea0d012`:

- `DatabaseConnection` and `DatabaseStatement` system interfaces for prepared execution without exposing Turso directly to domains.
- `DatabaseFactory` contracts for project and user-global scopes.
- `Migration`, `MigrationRegistry`, `MigrationRunner`, and `MigrationHistoryRepository` contracts.
- `TransactionRunner` and `BatchWriter` contracts with bounded transaction semantics.
- `ProjectLock`/single-instance guard contract.
- `GlobalProfileRepository` contract in the configuration domain.
- Turso-backed repository implementations for existing refinement and run domain repository interfaces.
- `SourceVersionRepository` primitive contract retained for future Feature 007
  memory-extension evidence tracking.

Contracts must be placed in the domain or system folders required by the repository architecture. Do not add global catch-all `types`, `interfaces`, or `enums` modules.
