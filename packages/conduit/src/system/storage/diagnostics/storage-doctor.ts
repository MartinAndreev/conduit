import { createTursoKysely } from "../adapters/kysely-turso-dialect.js";
import {
  GlobalDatabaseFactory,
  ProjectDatabaseFactory,
} from "../factories/database-factories.js";
import type { MigrationDatabase } from "../interfaces/migration-database.js";
import { BoundedBatchWriter } from "../repositories/transaction-runner.js";
import type { StorageDiagnostic } from "../types/storage-diagnostic.js";

async function migrationCount(
  database: ReturnType<typeof createTursoKysely<MigrationDatabase>>,
): Promise<number> {
  const result = await database
    .selectFrom("schema_migrations")
    .select(({ fn }) => fn.countAll<number>().as("count"))
    .executeTakeFirstOrThrow();
  return Number(result.count);
}

export async function verifyStorageRuntime(
  projectRoot: string,
  stateDirectory?: string,
  environment: NodeJS.ProcessEnv = process.env,
): Promise<StorageDiagnostic> {
  const projectFactory = new ProjectDatabaseFactory(
    projectRoot,
    undefined,
    stateDirectory,
  );
  const projectConnection = await projectFactory.open();
  const project = createTursoKysely<MigrationDatabase>(projectConnection);
  let projectMigrationCount: number;
  let interruptedMigrationId: string;
  try {
    await project.transaction().execute(async (transaction) => {
      const batch = new BoundedBatchWriter<string>(async (key) => {
        await transaction
          .insertInto("migration_recovery")
          .values({
            key,
            value: JSON.stringify({ verified: true }),
            updated_at: new Date().toISOString(),
          })
          .onConflict((conflict) =>
            conflict.column("key").doUpdateSet({
              value: JSON.stringify({ verified: true }),
              updated_at: new Date().toISOString(),
            }),
          )
          .execute();
      }, 4);
      await batch.writeBatch(["system:standalone-verification"]);
    });
    projectMigrationCount = await migrationCount(project);
    const latestMigration = await project
      .selectFrom("schema_migrations")
      .select("id")
      .where("scope", "=", "project")
      .orderBy("id", "desc")
      .executeTakeFirstOrThrow();
    interruptedMigrationId = latestMigration.id;
    await project
      .updateTable("schema_migrations")
      .set({ status: "running" })
      .where("id", "=", interruptedMigrationId)
      .execute();
  } finally {
    await project.destroy();
    await projectConnection.close();
  }

  const recoveryConnection = await projectFactory.open();
  const recoveryDatabase =
    createTursoKysely<MigrationDatabase>(recoveryConnection);
  let interruptedMigrationRecovered: boolean;
  try {
    const recovered = await recoveryDatabase
      .selectFrom("schema_migrations")
      .select("status")
      .where("id", "=", interruptedMigrationId)
      .executeTakeFirstOrThrow();
    interruptedMigrationRecovered = recovered.status === "succeeded";
  } finally {
    await recoveryDatabase.destroy();
    await recoveryConnection.close();
  }

  const globalFactory = new GlobalDatabaseFactory(environment);
  const globalConnection = await globalFactory.open();
  const global = createTursoKysely<MigrationDatabase>(globalConnection);
  let globalMigrationCount: number;
  try {
    globalMigrationCount = await migrationCount(global);
  } finally {
    await global.destroy();
    await globalConnection.close();
  }

  return {
    binding:
      process.env.CONDUIT_TURSO_BINDING ??
      environment.CONDUIT_TURSO_BINDING ??
      "dynamic-platform-binding",
    projectDatabase: projectConnection.databasePath,
    globalDatabase: globalConnection.databasePath,
    projectMigrationCount,
    globalMigrationCount,
    interruptedMigrationRecovered,
  };
}
