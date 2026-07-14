import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ProjectDatabaseFactory } from "../src/system/storage/factories/database-factories.js";
import { DefaultDatabaseLifecycle } from "../src/system/storage/repositories/database-lifecycle.js";
import type {
  DatabaseConnection,
  DatabaseStatement,
} from "../src/system/storage/interfaces/database.js";

test("database lifecycle finalizes, checkpoints, closes, and sanitizes failures", async () => {
  const operations: string[] = [];
  const diagnostics: string[] = [];
  const statement: DatabaseStatement = {
    execute: async () => ({ rows: [], rowsAffected: 0 }),
    all: async () => ({ rows: [], rowsAffected: 0 }),
    get: async () => undefined,
    finalize: async () => {
      operations.push("finalize");
    },
  };
  const connection: DatabaseConnection = {
    databasePath: "/private/state.db",
    execute: async () => ({ rows: [], rowsAffected: 0 }),
    prepare: async () => statement,
    backup: async () => {},
    checkpoint: async () => {
      operations.push("checkpoint");
      throw new Error("token=shutdown-secret");
    },
    close: async () => {
      operations.push("close");
    },
  };
  const lifecycle = new DefaultDatabaseLifecycle((diagnostic) =>
    diagnostics.push(diagnostic),
  );
  lifecycle.registerStatement(statement);
  lifecycle.registerConnection(connection);
  const first = await lifecycle.shutdown();
  const second = await lifecycle.shutdown();

  assert.deepEqual(operations, ["finalize", "checkpoint", "close"]);
  assert.equal(first, second);
  assert.equal(diagnostics.length, 1);
  assert.doesNotMatch(diagnostics[0] ?? "", /shutdown-secret/);
});

test("close finalizes statements, checkpoints, and releases project ownership", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-shutdown-"));
  try {
    const first = await new ProjectDatabaseFactory(projectRoot).open();
    const statement = await first.prepare(
      "SELECT id FROM schema_migrations ORDER BY id",
    );
    assert.ok((await statement.all()).rows.length > 0);
    await first.close();

    const second = await new ProjectDatabaseFactory(projectRoot).open();
    const reopened = await second.prepare(
      "SELECT id FROM schema_migrations ORDER BY id",
    );
    assert.ok((await reopened.all()).rows.length > 0);
    await reopened.finalize();
    await second.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("corrupt storage stops startup with scoped recovery guidance", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-corrupt-"));
  const stateDirectory = join(projectRoot, ".conduit");
  await mkdir(stateDirectory, { recursive: true });
  await writeFile(
    join(stateDirectory, "state.db"),
    "not-a-database token=corruption-secret",
  );
  try {
    await assert.rejects(
      () => new ProjectDatabaseFactory(projectRoot).open(),
      (error: Error & { remediation?: string; scope?: string }) =>
        error.scope === "project" &&
        Boolean(error.remediation?.includes("restore")) &&
        !error.message.includes("corruption-secret"),
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
