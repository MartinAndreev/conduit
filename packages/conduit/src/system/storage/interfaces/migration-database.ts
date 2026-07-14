import type { DatabaseScope } from "../types/database.js";
import type { MigrationStatus } from "../types/migration.js";

export interface MigrationHistoryTable {
  id: string;
  domain: string;
  scope: DatabaseScope;
  checksum: string;
  applied_at: string;
  duration_ms: number;
  status: MigrationStatus;
}

export interface MigrationRecoveryTable {
  key: string;
  value: string;
  updated_at: string;
}

export interface MigrationDatabase {
  schema_migrations: MigrationHistoryTable;
  migration_recovery: MigrationRecoveryTable;
}
