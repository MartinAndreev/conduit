import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createTursoKysely } from "../src/system/storage/adapters/kysely-turso-dialect.js";
import { ProjectDatabaseFactory } from "../src/system/storage/factories/database-factories.js";
import type { MigrationDatabase } from "../src/system/storage/interfaces/migration-database.js";
import { createDefaultMigrationRegistry } from "../src/system/storage/migrations/default-registry.js";
import { OrderedMigrationRegistry } from "../src/system/storage/migrations/migration-registry.js";
import { DefaultMigrationRunner } from "../src/system/storage/migrations/migration-runner.js";

test("migrations are ordered, checksummed, idempotent, and persistent", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-migrations-"));
  try {
    const first = await new ProjectDatabaseFactory(projectRoot).open();
    const database = createTursoKysely<MigrationDatabase>(first);
    const records = await database
      .selectFrom("schema_migrations")
      .selectAll()
      .orderBy("id")
      .execute();
    assert.deepEqual(
      records.map((record) => record.id),
      [
        "0001_system_project_metadata",
        "0003_refinement_state",
        "0004_runs_state",
        "0006_source_versions_primitives",
      ],
    );
    assert.ok(records.every((record) => record.checksum.length === 64));
    assert.ok(records.every((record) => record.status === "succeeded"));
    await database.destroy();
    await first.close();

    const second = await new ProjectDatabaseFactory(projectRoot).open();
    const result = await new DefaultMigrationRunner(
      createDefaultMigrationRegistry(),
    ).migrate(second, "project");
    assert.deepEqual(result.applied, []);
    const reopened = createTursoKysely<MigrationDatabase>(second);
    assert.equal(
      await reopened
        .selectFrom("schema_migrations")
        .select(({ fn }) => fn.countAll<number>().as("count"))
        .executeTakeFirstOrThrow()
        .then(({ count }) => Number(count)),
      4,
    );
    await reopened.destroy();
    await second.close();

    const backups = await readdir(join(projectRoot, ".conduit", "backups"));
    assert.ok(backups.some((name) => name.endsWith(".db")));
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("editing an applied migration fails with checksum remediation", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-checksum-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const defaults = createDefaultMigrationRegistry();
    const altered = new OrderedMigrationRegistry();
    for (const migration of defaults.list("project"))
      altered.register(
        migration.id === "0001_system_project_metadata"
          ? {
              ...migration,
              checksumSource: `${migration.checksumSource}-edited`,
            }
          : migration,
      );
    await assert.rejects(
      () => new DefaultMigrationRunner(altered).migrate(connection, "project"),
      /checksum does not match/,
    );
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
