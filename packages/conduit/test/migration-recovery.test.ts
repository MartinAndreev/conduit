import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createTursoKysely } from "../src/system/storage/adapters/kysely-turso-dialect.js";
import { openEmbeddedTursoConnection } from "../src/system/storage/adapters/embedded-turso.js";
import type { MigrationDatabase } from "../src/system/storage/interfaces/migration-database.js";
import { OrderedMigrationRegistry } from "../src/system/storage/migrations/migration-registry.js";
import { DefaultMigrationRunner } from "../src/system/storage/migrations/migration-runner.js";
import { migrationChecksum } from "../src/system/storage/migrations/migration-runner.js";
import { TursoMigrationHistoryRepository } from "../src/system/storage/migrations/migration-history-repository.js";
import type { MigrationDefinition } from "../src/system/storage/types/migration.js";

test("failed migration rolls back and can be retried safely", async () => {
  const directory = await mkdtemp(join(tmpdir(), "conduit-recovery-"));
  const databasePath = join(directory, "state.db");
  let shouldFail = true;
  const migration = {
    id: "0001_recovery_test",
    domain: "system",
    scope: "project",
    checksumSource: "recovery-test-v1",
    async up(database) {
      await database.schema
        .createTable("recovery_test")
        .addColumn("id", "integer", (column) => column.primaryKey())
        .execute();
      if (shouldFail) throw new Error("token=seeded-recovery-secret");
    },
  } satisfies MigrationDefinition;
  const registry = new OrderedMigrationRegistry();
  registry.register(migration);
  try {
    const connection = await openEmbeddedTursoConnection(
      "project",
      databasePath,
    );
    const runner = new DefaultMigrationRunner(registry);
    await assert.rejects(
      () => runner.migrate(connection, "project"),
      (error: Error) =>
        error.message.includes("Migration 0001_recovery_test failed") &&
        !error.message.includes("seeded-recovery-secret"),
    );
    const kysely = createTursoKysely<MigrationDatabase>(connection);
    const failed = await kysely
      .selectFrom("schema_migrations")
      .select("status")
      .where("id", "=", migration.id)
      .executeTakeFirstOrThrow();
    assert.equal(failed.status, "failed");
    await assert.rejects(
      () =>
        kysely
          .selectFrom("recovery_test" as "schema_migrations")
          .selectAll()
          .execute(),
      /no such table/,
    );

    shouldFail = false;
    const retried = await runner.migrate(connection, "project");
    assert.deepEqual(retried.applied, [migration.id]);
    await kysely.destroy();
    await connection.close();
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("startup detects a running migration record and retries it", async () => {
  const directory = await mkdtemp(join(tmpdir(), "conduit-interrupted-"));
  const migration = {
    id: "0001_interrupted_test",
    domain: "system",
    scope: "project",
    checksumSource: "interrupted-test-v1",
    async up(database) {
      await database.schema
        .createTable("interrupted_test")
        .addColumn("id", "integer", (column) => column.primaryKey())
        .execute();
    },
  } satisfies MigrationDefinition;
  const registry = new OrderedMigrationRegistry();
  registry.register(migration);
  try {
    const connection = await openEmbeddedTursoConnection(
      "project",
      join(directory, "state.db"),
    );
    const database = createTursoKysely<MigrationDatabase>(connection);
    const history = new TursoMigrationHistoryRepository(database);
    await history.initialize();
    await history.recordRunning(migration, migrationChecksum(migration));

    const result = await new DefaultMigrationRunner(registry).migrate(
      connection,
      "project",
    );
    assert.deepEqual(result.recovered, [migration.id]);
    assert.deepEqual(result.applied, [migration.id]);
    await database.destroy();
    await connection.close();
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
