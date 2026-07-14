import type { DatabaseConnection } from "./database.js";
import type {
  StartupMigrationProgress,
  StartupMigrationSummary,
} from "../types/startup-migration.js";

export interface LegacyImportResult {
  readonly importedRecords: number;
  readonly skippedImports: number;
}

export interface LegacyImportRunner {
  import(connection: DatabaseConnection): Promise<LegacyImportResult>;
}

export interface StartupMigrationRunner {
  run(
    onProgress?: (progress: StartupMigrationProgress) => void,
  ): Promise<StartupMigrationSummary>;
}
