export type StartupMigrationStage =
  "global-schema" | "project-schema" | "legacy-import" | "complete";

export type StartupMigrationProgress = Readonly<{
  stage: StartupMigrationStage;
  message: string;
  completed: number;
  total: number;
}>;

export type StartupMigrationSummary = Readonly<{
  globalMigrations: readonly string[];
  projectMigrations: readonly string[];
  importedRecords: number;
  skippedImports: number;
}>;
