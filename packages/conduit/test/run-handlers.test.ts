import { test } from "bun:test";
import assert from "node:assert/strict";
import { InMemoryRunEventRepository } from "../src/domains/runs/repositories/in-memory-run-event-repository.js";
import { InMemoryReviewResultRepository } from "../src/domains/runs/repositories/in-memory-review-result-repository.js";
import { createCancelRunHandler } from "../src/domains/runs/handlers/cancel-run-handler.js";
import { createGetRunEventsHandler } from "../src/domains/runs/handlers/get-run-events-handler.js";
import { createReviewRunHandler } from "../src/domains/runs/handlers/review-run-handler.js";
import { createGetReviewResultHandler } from "../src/domains/runs/handlers/get-review-result-handler.js";
import { createGetRunDiffHandler } from "../src/domains/runs/handlers/get-run-diff-handler.js";
import { createResumeRunHandler } from "../src/domains/runs/handlers/resume-run-handler.js";
import { createEvent } from "../src/system/runners/events.js";
import { createRunProcessRegistry } from "../src/domains/runs/repositories/run-process-registry.js";
import { CommandBus } from "../src/system/bus/command-bus.js";
import type { LifecyclePayload } from "../src/domains/runs/types/runner-events.js";
import type { RunEventRepository } from "../src/domains/runs/interfaces/run-event-repository.js";
import type { DiffReader } from "../src/domains/runs/interfaces/diff-reader.js";
import type { RunRecoveryRepository } from "../src/domains/runs/interfaces/run-recovery-repository.js";
import type { Run } from "../src/domains/runs/types/run.js";

function recoveryRepository(run?: Run): RunRecoveryRepository {
  return {
    saveSnapshot: async () => {
      throw new Error("not used");
    },
    claimFailedRun: async () => undefined,
    loadSnapshot: async () =>
      run
        ? {
            run,
            state: "running",
            version: 1,
            updatedAt: "2026-01-01T00:00:00.000Z",
          }
        : undefined,
    listSnapshots: async () => [],
    markInterrupted: async () => {},
    markCancelled: async () => {},
  };
}

test("resumeRun dispatches through CommandBus and persists the resumed snapshot", async () => {
  const run: Run = {
    id: "run-failed",
    featureId: "001",
    status: "failed",
    createdAt: "2026-01-01T00:00:00.000Z",
    roles: [
      {
        name: "reviewer",
        runner: "codex",
        readOnly: true,
        owns: [],
        dependsOn: [],
        promptFile: "",
        prompt: "",
        command: "",
        args: [],
        skillSource: "test",
        status: "completed",
      },
    ],
    stateDirectory: "/tmp/conduit-state",
  };
  let version = 1;
  let saved = 0;
  const repository: RunRecoveryRepository = {
    loadSnapshot: async () => ({
      run,
      state: "complete",
      version,
      updatedAt: "2026-01-01T00:00:00.000Z",
    }),
    claimFailedRun: async (_runId, expectedVersion) => {
      assert.equal(expectedVersion, version);
      version += 1;
      run.status = "running";
      return {
        run,
        state: "running",
        version,
        updatedAt: "2026-01-01T00:00:00.000Z",
      };
    },
    saveSnapshot: async (nextRun, expectedVersion) => {
      assert.equal(expectedVersion, version);
      saved += 1;
      version += 1;
      return {
        run: nextRun,
        state: "complete",
        version,
        updatedAt: "2026-01-01T00:00:00.000Z",
      };
    },
    listSnapshots: async () => [],
    markInterrupted: async () => {},
    markCancelled: async () => {},
  };
  const handler = createResumeRunHandler(repository, {
    projectRoot: "/tmp/project",
    evaluateEligibility: async () => ({
      state: "resumable",
      preservedRoles: [],
      retryRoles: ["reviewer"],
      reconstructRoles: [],
    }),
    executeRun: async (input) => {
      assert.equal(input.resume, true);
      assert.equal(input.run, run);
      assert.equal(input.run.roles[0]?.status, "failed");
      input.run.status = "completed";
      await input.onRoleWorkspaceReady?.();
      return [];
    },
  });
  const bus = new CommandBus();
  bus.register("resumeRun", handler);

  const result = await bus.dispatch({ type: "resumeRun", runId: run.id });

  assert.equal(result.success, true);
  assert.equal(run.status, "completed");
  assert.equal(saved, 2);
});

// createCancelRunHandler

test("createCancelRunHandler appends cancelled lifecycle event", async () => {
  const repo = new InMemoryRunEventRepository();
  const registry = createRunProcessRegistry();
  const handler = createCancelRunHandler(repo, registry);
  const result = await handler({ type: "cancelRun", runId: "run-1" });
  assert.equal(result.success, true);
  if (result.success) assert.equal(result.data.cancelled, true);
  const events = await repo.loadByRun("run-1");
  assert.equal(events.length, 1);
  assert.equal(events[0]!.type, "lifecycle");
  const payload = events[0]!.payload as LifecyclePayload;
  assert.equal(payload.state, "cancelled");
});

test("cancelRun dispatches through CommandBus", async () => {
  const repo = new InMemoryRunEventRepository();
  const registry = createRunProcessRegistry();
  const bus = new CommandBus();
  bus.register("cancelRun", createCancelRunHandler(repo, registry));
  const result = await bus.dispatch({ type: "cancelRun", runId: "r1" });
  assert.equal(result.success, true);
  assert.equal((await repo.loadByRun("r1")).length, 1);
});

// createGetRunEventsHandler

test("createGetRunEventsHandler returns events and roleIds", async () => {
  const repo = new InMemoryRunEventRepository();
  await repo.append(
    createEvent("lifecycle", "r1", "a", {
      kind: "lifecycle",
      state: "starting",
    }),
  );
  await repo.append(
    createEvent("activity", "r1", "b", { kind: "activity", message: "x" }),
  );
  const handler = createGetRunEventsHandler(repo);
  const result = await handler({ type: "getRunEvents", runId: "r1" });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as unknown as {
      events: unknown[];
      roleIds: string[];
    };
    assert.equal(data.events.length, 2);
    assert.deepEqual(data.roleIds.sort(), ["a", "b"]);
  }
});

test("createGetRunEventsHandler filters by roleId", async () => {
  const repo = new InMemoryRunEventRepository();
  await repo.append(
    createEvent("activity", "r1", "a", { kind: "activity", message: "1" }),
  );
  await repo.append(
    createEvent("activity", "r1", "b", { kind: "activity", message: "2" }),
  );
  const handler = createGetRunEventsHandler(repo);
  const result = await handler({
    type: "getRunEvents",
    runId: "r1",
    roleId: "a",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as unknown as { events: unknown[] };
    assert.equal(data.events.length, 1);
  }
});

test("createGetRunEventsHandler accepts RunEventRepository interface", async () => {
  const calls: string[] = [];
  const mockRepo: RunEventRepository = {
    async append() {},
    async loadByRun() {
      calls.push("loadByRun");
      return [];
    },
    async loadByRole() {
      return [];
    },
    async loadRoleIds() {
      calls.push("loadRoleIds");
      return [];
    },
    async clear() {},
  };
  const handler = createGetRunEventsHandler(mockRepo);
  await handler({ type: "getRunEvents", runId: "r1" });
  assert.ok(calls.includes("loadByRun"));
  assert.ok(calls.includes("loadRoleIds"));
});

// createReviewRunHandler

test("createReviewRunHandler persists decision and findings", async () => {
  const repo = new InMemoryReviewResultRepository();
  const handler = createReviewRunHandler(repo);
  const result = await handler({
    type: "reviewRun",
    projectRoot: "/tmp",
    featureId: "001",
    runId: "r1",
    decision: "approved",
    findings: [
      { severity: "warning", file: "src/foo.ts", line: 10, message: "unused" },
    ],
    followUp: "fix it",
  });
  assert.equal(result.success, true);
  if (result.success) {
    assert.equal(result.data.decision, "approved");
    assert.equal(result.data.findingsCount, 1);
    assert.deepEqual(result.data.evidencePaths, ["src/foo.ts"]);
    assert.equal(result.data.followUp, "fix it");
  }
  const saved = await repo.load("r1");
  assert.ok(saved);
  assert.equal(saved!.decision, "approved");
});

test("reviewRun dispatches through CommandBus", async () => {
  const repo = new InMemoryReviewResultRepository();
  const bus = new CommandBus();
  bus.register("reviewRun", createReviewRunHandler(repo));
  const result = await bus.dispatch({
    type: "reviewRun",
    projectRoot: "/tmp",
    featureId: "001",
    runId: "r1",
    decision: "rejected",
    findings: [],
  });
  assert.equal(result.success, true);
});

// createGetReviewResultHandler

test("createGetReviewResultHandler returns empty for missing review", async () => {
  const repo = new InMemoryReviewResultRepository();
  const handler = createGetReviewResultHandler(repo);
  const result = await handler({ type: "getReviewResult", runId: "missing" });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as unknown as { review: { reviewId: undefined } };
    assert.equal(data.review.reviewId, undefined);
  }
});

test("createGetReviewResultHandler returns persisted review", async () => {
  const repo = new InMemoryReviewResultRepository();
  await repo.save({
    reviewId: "rev-1",
    runId: "r1",
    featureId: "001",
    decision: "rejected",
    findings: [{ severity: "error", message: "bad" }],
    evidencePaths: [],
    followUp: undefined,
    reviewedAt: "2026-01-01",
  });
  const handler = createGetReviewResultHandler(repo);
  const result = await handler({ type: "getReviewResult", runId: "r1" });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as unknown as {
      review: { decision: string; findings: unknown[] };
    };
    assert.equal(data.review.decision, "rejected");
    assert.equal(data.review.findings.length, 1);
  }
});

// createGetRunDiffHandler

test("createGetRunDiffHandler resolves worktree and baseline from persisted run data", async () => {
  let receivedBaseline: string | undefined;
  const mockReader: DiffReader = {
    readDiff(_worktree, baseline) {
      receivedBaseline = baseline;
      return {
        diff: "diff",
        changedFiles: [{ path: "a.ts", additions: 1, deletions: 0 }],
        totalAdditions: 1,
        totalDeletions: 0,
      };
    },
  };
  const handler = createGetRunDiffHandler(
    mockReader,
    recoveryRepository({
      id: "r1",
      featureId: "002",
      status: "running",
      createdAt: "2026-01-01T00:00:00.000Z",
      roles: [
        {
          name: "be",
          runner: "codex",
          readOnly: false,
          owns: [],
          dependsOn: [],
          promptFile: "prompt.md",
          prompt: "prompt",
          command: "codex",
          args: [],
          skillSource: "builtin",
          status: "running",
          worktree: "/worktree",
          diffBaselineHead: "baseline-head",
        },
      ],
    }),
  );
  const result = await handler({
    type: "getRunDiff",
    projectRoot: "/p",
    runId: "r1",
    roleId: "be",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as unknown as {
      diff: string | undefined;
      changedFiles: unknown[];
    };
    assert.equal(data.diff, "diff");
    assert.equal(data.changedFiles.length, 1);
    assert.equal(receivedBaseline, "baseline-head");
  }
});

test("createGetRunDiffHandler returns empty diff when no worktree in run", async () => {
  const mockReader: DiffReader = {
    readDiff() {
      return {
        diff: "diff",
        changedFiles: [],
        totalAdditions: 0,
        totalDeletions: 0,
      };
    },
  };
  const handler = createGetRunDiffHandler(mockReader, recoveryRepository());
  const result = await handler({
    type: "getRunDiff",
    projectRoot: "/p",
    runId: "nonexistent",
    roleId: "be",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as unknown as { diff: string | undefined };
    assert.equal(data.diff, undefined);
  }
});

// CQRS bus integration

test("reviewRun and cancelRun wired through CommandBus", async () => {
  const eventRepo = new InMemoryRunEventRepository();
  const reviewRepo = new InMemoryReviewResultRepository();
  const registry = createRunProcessRegistry();
  const bus = new CommandBus();
  bus.register("cancelRun", createCancelRunHandler(eventRepo, registry));
  bus.register("reviewRun", createReviewRunHandler(reviewRepo));

  await bus.dispatch({ type: "cancelRun", runId: "r1" });
  assert.equal((await eventRepo.loadByRun("r1")).length, 1);

  const reviewResult = await bus.dispatch({
    type: "reviewRun",
    projectRoot: "/tmp",
    featureId: "001",
    runId: "r2",
    decision: "approved",
    findings: [],
  });
  assert.equal(reviewResult.success, true);
});

test("getRunEvents and getReviewResult wired through QueryBus", async () => {
  const eventRepo = new InMemoryRunEventRepository();
  const reviewRepo = new InMemoryReviewResultRepository();
  const { QueryBus } = await import("../src/system/bus/query-bus.js");
  const bus = new QueryBus();
  bus.register("getRunEvents", createGetRunEventsHandler(eventRepo));
  bus.register("getReviewResult", createGetReviewResultHandler(reviewRepo));

  await eventRepo.append(
    createEvent("lifecycle", "r1", "a", {
      kind: "lifecycle",
      state: "completed",
    }),
  );

  const eventsResult = await bus.execute({ type: "getRunEvents", runId: "r1" });
  assert.equal(eventsResult.success, true);
  if (eventsResult.success) {
    const data = eventsResult.data as unknown as { events: unknown[] };
    assert.equal(data.events.length, 1);
  }

  const reviewResult = await bus.execute({
    type: "getReviewResult",
    runId: "r1",
  });
  assert.equal(reviewResult.success, true);
});
