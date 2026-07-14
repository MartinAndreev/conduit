export type {
  DatabaseConnection,
  DatabaseStatement,
  TransactionRunner,
  BatchWriter,
  ShutdownHook,
} from "./interfaces/database.js";
export type { DatabaseFactory } from "./interfaces/factory.js";
export type { DatabaseLifecycle } from "./interfaces/database-lifecycle.js";
export type {
  MigrationRegistry,
  MigrationRunner,
  MigrationHistoryRepository,
} from "./interfaces/migration.js";
export type {
  ProjectLock,
  ProjectLockFactory,
} from "./interfaces/project-lock.js";
export type {
  DatabaseScope,
  SqlParameter,
  SqlParameters,
  QueryResult,
  QueryResultRow,
  DatabasePathSet,
} from "./types/database.js";
export type {
  MigrationDefinition,
  MigrationStatus,
  AppliedMigration,
  MigrationResult,
} from "./types/migration.js";
export {
  StorageError,
  redactStorageDiagnostic,
  toStorageError,
} from "./errors/storage-error.js";
export {
  resolveProjectDatabasePaths,
  resolveGlobalDatabasePaths,
} from "./factories/path-resolution.js";
export { ensureConduitStateGitIgnored } from "./factories/gitignore.js";
export {
  ProjectDatabaseFactory,
  GlobalDatabaseFactory,
} from "./factories/database-factories.js";
export {
  FileProjectLock,
  FileProjectLockFactory,
} from "./repositories/project-lock.js";
export {
  DatabaseTransactionRunner,
  BoundedBatchWriter,
} from "./repositories/transaction-runner.js";
export { DefaultDatabaseLifecycle } from "./repositories/database-lifecycle.js";
export {
  EmbeddedTursoConnection,
  EmbeddedTursoStatement,
  openEmbeddedTursoConnection,
} from "./adapters/embedded-turso.js";
export { OrderedMigrationRegistry } from "./migrations/migration-registry.js";
export {
  DefaultMigrationRunner,
  migrationChecksum,
} from "./migrations/migration-runner.js";
export { createDefaultMigrationRegistry } from "./migrations/default-registry.js";
