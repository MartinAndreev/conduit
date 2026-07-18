import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { TursoRunEventRepository } from "../../src/domains/runs/repositories/turso-run-event-repository.js";
import { TursoRunRecoveryRepository } from "../../src/domains/runs/repositories/turso-run-recovery-repository.js";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";
import { RunnerEventProvenance } from "../../src/domains/runs/enums/runner-event-provenance.js";

test("Turso run events keep deterministic order under concurrent appends", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-events-db-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRunEventRepository(connection);
    await Promise.all(
      Array.from({ length: 20 }, (_, index) =>
        repository.append({
          type: "activity",
          provenance: RunnerEventProvenance.ConduitObserved,
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
    for (let index = 0; index < 25; index += 1)
      await repository.saveSnapshot({
        ...run,
        id: `history-${index}`,
        createdAt: `2026-01-${String(index + 2).padStart(2, "0")}T00:00:00.000Z`,
      });
    assert.equal((await repository.listSnapshots()).length, 26);
    assert.equal((await repository.listSnapshots(20)).length, 20);
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("Turso failed-run claim is atomic and rejects a second resume", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-claim-db-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRunRecoveryRepository(connection);
    const snapshot = await repository.saveSnapshot({
      id: "run-claim",
      featureId: "008",
      status: "failed",
      createdAt: "2026-01-01T00:00:00.000Z",
      roles: [],
    });
    const [first, second] = await Promise.all([
      repository.claimFailedRun("run-claim", snapshot.version),
      repository.claimFailedRun("run-claim", snapshot.version),
    ]);
    assert.equal([first, second].filter(Boolean).length, 1);
    assert.equal(
      (await repository.loadSnapshot("run-claim"))?.run.status,
      "running",
    );
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("Turso run snapshots exclude duplicated prompt and context bodies", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-compact-run-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRunRecoveryRepository(connection);
    await repository.saveSnapshot({
      id: "run-compact",
      featureId: "007",
      status: "planned",
      createdAt: "2026-01-01T00:00:00.000Z",
      roles: [
        {
          name: "backend",
          runner: "codex",
          readOnly: false,
          owns: ["src"],
          dependsOn: [],
          promptFile: ".state/runs/run-compact/backend-assignment.json",
          prompt: "duplicated assignment body",
          context: "duplicated context body",
          command: "codex",
          args: [],
          skillSource: "test",
          status: "planned",
        },
      ],
    });
    const stored = await repository.loadSnapshot("run-compact");
    assert.equal(stored?.run.roles[0]?.prompt, "");
    assert.equal(stored?.run.roles[0]?.context, undefined);
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
