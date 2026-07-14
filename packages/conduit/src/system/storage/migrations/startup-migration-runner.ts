import type { LegacyImportRunner } from "../interfaces/startup-migration.js";
import type { StartupMigrationProgress } from "../types/startup-migration.js";
import {
  GlobalDatabaseFactory,
  ProjectDatabaseFactory,
} from "../factories/database-factories.js";
import { createDefaultMigrationRegistry } from "./default-registry.js";
import { DefaultMigrationRunner } from "./migration-runner.js";

export class DefaultStartupMigrationRunner {
  constructor(
    private readonly projectRoot: string,
    private readonly stateDirectory?: string,
    private readonly legacyImporter?: LegacyImportRunner,
    private readonly environment: NodeJS.ProcessEnv = process.env,
  ) {}

  async run(
    onProgress: (progress: StartupMigrationProgress) => void = () => {},
  ) {
    const registry = createDefaultMigrationRegistry();
    const runner = new DefaultMigrationRunner(registry);
    onProgress({
      stage: "global-schema",
      message: "Preparing user-global storage",
      completed: 0,
      total: 3,
    });
    const globalFactory = new GlobalDatabaseFactory(this.environment);
    const globalConnection = await globalFactory.openWithoutMigrations();
    let globalMigrations: readonly string[];
    try {
      globalMigrations = (await runner.migrate(globalConnection, "global"))
        .applied;
    } finally {
      await globalConnection.close();
    }

    onProgress({
      stage: "project-schema",
      message: "Preparing project storage",
      completed: 1,
      total: 3,
    });
    const projectFactory = new ProjectDatabaseFactory(
      this.projectRoot,
      undefined,
      this.stateDirectory,
    );
    const projectConnection = await projectFactory.openWithoutMigrations();
    let projectMigrations: readonly string[];
    let importedRecords = 0;
    let skippedImports = 0;
    try {
      projectMigrations = (await runner.migrate(projectConnection, "project"))
        .applied;
      onProgress({
        stage: "legacy-import",
        message: "Importing existing local project state",
        completed: 2,
        total: 3,
      });
      if (this.legacyImporter) {
        const imported = await this.legacyImporter.import(projectConnection);
        importedRecords = imported.importedRecords;
        skippedImports = imported.skippedImports;
      }
    } finally {
      await projectConnection.close();
    }

    onProgress({
      stage: "complete",
      message: "Storage is ready",
      completed: 3,
      total: 3,
    });
    return {
      globalMigrations,
      projectMigrations,
      importedRecords,
      skippedImports,
    };
  }
}
