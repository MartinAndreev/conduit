import type { StartupMigrationStage } from "@system/storage/types/startup-migration.js";

export interface MigrationScreenState {
  readonly stage: StartupMigrationStage;
  readonly message: string;
  readonly completed: number;
  readonly total: number;
  readonly error?: string;
}
