import { test } from "bun:test";
import assert from "node:assert/strict";
import { InMemoryRunEventRepository } from "../src/domains/runs/repositories/in-memory-run-event-repository.js";
import { createEvent } from "../src/system/runners/events.js";
import type { RunnerEvent } from "../src/domains/runs/types/runner-events.js";
import type { RunEventRepository } from "../src/domains/runs/interfaces/run-event-repository.js";
import { RunnerEventProvenance } from "../src/domains/runs/enums/runner-event-provenance.js";

test("append and loadByRun return events for the target run", async () => {
  const repo = new InMemoryRunEventRepository();
  await repo.append(
    createEvent("activity", "r1", "backend", {
      kind: "activity",
      message: "a1",
    }),
  );
  await repo.append(
    createEvent("activity", "r2", "frontend", {
      kind: "activity",
      message: "b1",
    }),
  );

  const r1 = await repo.loadByRun("r1");
  assert.equal(r1.length, 1);
  assert.equal(r1[0]!.roleId, "backend");
});

test("loadByRole filters by both runId and roleId", async () => {
  const repo = new InMemoryRunEventRepository();
  await repo.append(
    createEvent("lifecycle", "r1", "backend", {
      kind: "lifecycle",
      state: "starting",
    }),
  );
  await repo.append(
    createEvent("activity", "r1", "backend", {
      kind: "activity",
      message: "working",
    }),
  );
  await repo.append(
    createEvent("lifecycle", "r1", "frontend", {
      kind: "lifecycle",
      state: "starting",
    }),
  );

  const backend = await repo.loadByRole("r1", "backend");
  assert.equal(backend.length, 2);
  assert.ok(backend.every((e) => e.roleId === "backend"));
});

test("loadRoleIds returns distinct role ids for a run", async () => {
  const repo = new InMemoryRunEventRepository();
  await repo.append(
    createEvent("activity", "r1", "backend", {
      kind: "activity",
      message: "a1",
    }),
  );
  await repo.append(
    createEvent("activity", "r1", "frontend", {
      kind: "activity",
      message: "b1",
    }),
  );
  await repo.append(
    createEvent("activity", "r1", "backend", {
      kind: "activity",
      message: "a2",
    }),
  );
  await repo.append(
    createEvent("activity", "r2", "qa", { kind: "activity", message: "c1" }),
  );

  const roleIds = await repo.loadRoleIds("r1");
  assert.deepEqual([...roleIds].sort(), ["backend", "frontend"]);
});

test("loadRoleIds returns empty array for unknown run", async () => {
  const repo = new InMemoryRunEventRepository();
  const roleIds = await repo.loadRoleIds("unknown");
  assert.equal(roleIds.length, 0);
});

test("clear removes only events for the target run", async () => {
  const repo = new InMemoryRunEventRepository();
  await repo.append(
    createEvent("activity", "r1", "a", { kind: "activity", message: "a1" }),
  );
  await repo.append(
    createEvent("activity", "r2", "b", { kind: "activity", message: "b1" }),
  );
  await repo.append(
    createEvent("activity", "r1", "a", { kind: "activity", message: "a2" }),
  );

  await repo.clear("r1");
  assert.equal((await repo.loadByRun("r1")).length, 0);
  assert.equal((await repo.loadByRun("r2")).length, 1);
});

test("implements RunEventRepository interface", () => {
  const repo = new InMemoryRunEventRepository();
  const check: RunEventRepository = repo;
  assert.ok(typeof check.append === "function");
  assert.ok(typeof check.loadByRun === "function");
  assert.ok(typeof check.loadByRole === "function");
  assert.ok(typeof check.loadRoleIds === "function");
  assert.ok(typeof check.clear === "function");
});

test("stores all discriminated runner event types", async () => {
  const repo = new InMemoryRunEventRepository();
  const events: RunnerEvent[] = [
    {
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: "r",
      roleId: "role",
      timestamp: "",
      payload: { kind: "lifecycle", state: "running" },
    },
    {
      type: "activity",
      provenance: RunnerEventProvenance.RunnerReported,
      runId: "r",
      roleId: "role",
      timestamp: "",
      payload: { kind: "activity", message: "doing stuff" },
    },
    {
      type: "tool-call",
      provenance: RunnerEventProvenance.RunnerReported,
      runId: "r",
      roleId: "role",
      timestamp: "",
      payload: { kind: "tool-call", tool: "read" },
    },
    {
      type: "tool-output",
      provenance: RunnerEventProvenance.RunnerReported,
      runId: "r",
      roleId: "role",
      timestamp: "",
      payload: {
        kind: "tool-output",
        tool: "read",
        output: "contents",
        truncated: false,
      },
    },
    {
      type: "file-change",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: "r",
      roleId: "role",
      timestamp: "",
      payload: {
        kind: "file-change",
        path: "src/index.ts",
        additions: 5,
        deletions: 2,
      },
    },
    {
      type: "error",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: "r",
      roleId: "role",
      timestamp: "",
      payload: {
        kind: "error",
        code: "FAIL",
        message: "oops",
        recoverable: true,
      },
    },
    {
      type: "result",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: "r",
      roleId: "role",
      timestamp: "",
      payload: { kind: "result", exitCode: 0, files: [], summary: "done" },
    },
  ];

  for (const event of events) await repo.append(event);
  const all = await repo.loadByRun("r");
  assert.equal(all.length, 7);
  const types = all.map((e) => e.type);
  assert.ok(types.includes("lifecycle"));
  assert.ok(types.includes("activity"));
  assert.ok(types.includes("tool-call"));
  assert.ok(types.includes("tool-output"));
  assert.ok(types.includes("file-change"));
  assert.ok(types.includes("error"));
  assert.ok(types.includes("result"));
});
