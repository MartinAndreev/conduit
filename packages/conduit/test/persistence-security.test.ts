import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { TursoDraftRepository } from "../src/domains/refinement/repositories/turso-draft-repository.js";
import { TursoResearchReportRepository } from "../src/domains/refinement/repositories/turso-research-report-repository.js";
import { TursoRunEventRepository } from "../src/domains/runs/repositories/turso-run-event-repository.js";
import {
  GlobalDatabaseFactory,
  ProjectDatabaseFactory,
} from "../src/system/storage/factories/database-factories.js";

test("seeded secrets are absent from project database and backups", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-security-"));
  const environmentSecret = "environment-secret-value-987654";
  const previous = process.env.CONDUIT_TEST_TOKEN;
  process.env.CONDUIT_TEST_TOKEN = environmentSecret;
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    await new TursoDraftRepository(connection).save({
      featureId: "001",
      story: `password=project-secret ${environmentSecret}`,
      testCases: "Bearer bearer-secret-value",
      createdAt: "2026-01-01T00:00:00.000Z",
      updatedAt: "2026-01-01T00:00:00.000Z",
    });
    await new TursoResearchReportRepository(connection).save(
      "001",
      "api_key=research-secret-value",
    );
    await new TursoRunEventRepository(connection).append({
      type: "tool-output",
      runId: "run-1",
      roleId: "backend",
      timestamp: "2026-01-01T00:00:00.000Z",
      payload: {
        kind: "tool-output",
        tool: "runner",
        output: "token=event-secret-value",
        truncated: false,
      },
    });
    const backupPath = join(projectRoot, ".conduit", "security-backup.db");
    await connection.checkpoint();
    await connection.backup(backupPath);
    await connection.close();

    const migrationBackups = (
      await readdir(join(projectRoot, ".conduit", "backups"))
    ).map((name) => join(projectRoot, ".conduit", "backups", name));
    const globalRoot = join(projectRoot, "global-data");
    const globalConnection = await new GlobalDatabaseFactory({
      ...process.env,
      XDG_DATA_HOME: globalRoot,
      APPDATA: globalRoot,
    }).open();
    const globalDatabasePath = globalConnection.databasePath;
    await globalConnection.close();
    const globalMigrationBackups = (
      await readdir(join(dirname(globalDatabasePath), "backups"))
    ).map((name) => join(dirname(globalDatabasePath), "backups", name));

    for (const file of [
      join(projectRoot, ".conduit", "state.db"),
      backupPath,
      ...migrationBackups,
      globalDatabasePath,
      ...globalMigrationBackups,
    ]) {
      const bytes = await readFile(file);
      for (const secret of [
        "project-secret",
        "bearer-secret-value",
        "research-secret-value",
        "event-secret-value",
        environmentSecret,
      ])
        assert.equal(
          bytes.includes(secret),
          false,
          `${secret} leaked into ${file}`,
        );
    }
  } finally {
    if (previous === undefined) delete process.env.CONDUIT_TEST_TOKEN;
    else process.env.CONDUIT_TEST_TOKEN = previous;
    await rm(projectRoot, { recursive: true, force: true });
  }
});
