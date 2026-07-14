import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { TursoRunEventRepository } from "../../src/domains/runs/repositories/turso-run-event-repository.js";
import { TursoRunRecoveryRepository } from "../../src/domains/runs/repositories/turso-run-recovery-repository.js";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";

test("Turso run events keep deterministic order under concurrent appends", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-events-db-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRunEventRepository(connection);
    await Promise.all(
      Array.from({ length: 20 }, (_, index) =>
        repository.append({
          type: "activity",
          runId: "run-1",
          roleId: index % 2 ? "backend" : "frontend",
          timestamp: new Date(index).toISOString(),
          payload: { kind: "activity", message: `event-${index}` },
        }),
      ),
    );
    const events = await repository.loadByRun("run-1");
    assert.equal(events.length, 20);
    assert.equal(
      new Set(
        events.map((event) =>
          event.payload.kind === "activity" ? event.payload.message : "",
        ),
      ).size,
      20,
    );
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("Turso run snapshots reject stale versions and list recent runs", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-snapshot-db-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRunRecoveryRepository(connection);
    const run = {
      id: "run-1",
      featureId: "001",
      status: "planned" as const,
      createdAt: "2026-01-01T00:00:00.000Z",
      roles: [],
    };
    await repository.saveSnapshot(run);
    await repository.saveSnapshot({ ...run, status: "completed" }, 1);
    await assert.rejects(
      () => repository.saveSnapshot({ ...run, status: "failed" }, 1),
      /updated by another operation/,
    );
    assert.equal(
      (await repository.listSnapshots())[0]?.run.status,
      "completed",
    );
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
