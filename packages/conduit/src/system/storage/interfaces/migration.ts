import type { DatabaseConnection } from "./database.js";
import type {
  AppliedMigration,
  MigrationDefinition,
  MigrationResult,
} from "../types/migration.js";
import type { DatabaseScope } from "../types/database.js";

export interface MigrationRegistry {
  register(migration: MigrationDefinition): void;
  list(scope: DatabaseScope): readonly MigrationDefinition[];
}

export interface MigrationHistoryRepository {
  initialize(): Promise<void>;
  loadAll(): Promise<readonly AppliedMigration[]>;
  recordRunning(
    migration: MigrationDefinition,
    checksum: string,
  ): Promise<void>;
  recordFinished(
    migration: MigrationDefinition,
    checksum: string,
    durationMs: number,
    succeeded: boolean,
  ): Promise<void>;
}

export interface MigrationRunner {
  migrate(
    connection: DatabaseConnection,
    scope: DatabaseScope,
  ): Promise<MigrationResult>;
}
