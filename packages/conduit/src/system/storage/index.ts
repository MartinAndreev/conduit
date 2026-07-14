export type {
  DatabaseConnection,
  DatabaseStatement,
  TransactionRunner,
  BatchWriter,
  ShutdownHook,
} from "./interfaces/database.js";
export type { DatabaseFactory } from "./interfaces/factory.js";
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
export {
  EmbeddedTursoConnection,
  EmbeddedTursoStatement,
  openEmbeddedTursoConnection,
} from "./adapters/embedded-turso.js";
