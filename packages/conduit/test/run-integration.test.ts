import { test } from "bun:test";
import assert from "node:assert/strict";
import { FileRunEventRepository } from "../src/domains/runs/repositories/file-run-event-repository.js";
import { FileReviewResultRepository } from "../src/domains/runs/repositories/file-review-result-repository.js";
import { createRunProcessRegistry } from "../src/domains/runs/repositories/run-process-registry.js";
import { createCancelRunHandler } from "../src/domains/runs/handlers/cancel-run-handler.js";
import { InMemoryRunEventRepository } from "../src/domains/runs/repositories/in-memory-run-event-repository.js";
import {
  deriveRolePresentation,
  extractFileDiff,
} from "../src/tui/helpers/event-presentation.js";
import { createEvent } from "../src/system/runners/events.js";
import type {
  RunnerEvent,
  LifecyclePayload,
} from "../src/domains/runs/types/runner-events.js";
import type { Run } from "../src/domains/runs/types/run.js";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

// File-backed run event repository integration

test("FileRunEventRepository persists events to disk and loads them back", async () => {
  const dir = await mkdtemp(path.join(tmpdir(), "conduit-test-"));
  try {
    const repo = new FileRunEventRepository(dir);
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

    const events = await repo.loadByRun("r1");
    assert.equal(events.length, 3);
    assert.equal(events[0]!.type, "lifecycle");

    const roleIds = await repo.loadRoleIds("r1");
    assert.deepEqual([...roleIds].sort(), ["backend", "frontend"]);

    const backend = await repo.loadByRole("r1", "backend");
    assert.equal(backend.length, 2);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("FileRunEventRepository preserves concurrently appended events", async () => {
  const dir = await mkdtemp(path.join(tmpdir(), "conduit-test-"));
  try {
    const repo = new FileRunEventRepository(dir);
    await Promise.all(
      Array.from({ length: 24 }, (_, index) =>
        repo.append(
          createEvent("activity", "r1", "researcher", {
            kind: "activity",
            message: `event ${index}`,
          }),
        ),
      ),
    );
    const events = await repo.loadByRun("r1");
    assert.equal(events.length, 24);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("FileRunEventRepository clear empties events for the target run", async () => {
  const dir = await mkdtemp(path.join(tmpdir(), "conduit-test-"));
  try {
    const repo = new FileRunEventRepository(dir);
    await repo.append(
      createEvent("activity", "r1", "a", { kind: "activity", message: "a1" }),
    );
    await repo.append(
      createEvent("activity", "r2", "b", { kind: "activity", message: "b1" }),
    );

    await repo.clear("r1");
    assert.equal((await repo.loadByRun("r1")).length, 0);
    assert.equal((await repo.loadByRun("r2")).length, 1);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

// File-backed review result repository integration

test("FileReviewResultRepository persists and loads review results", async () => {
  const dir = await mkdtemp(path.join(tmpdir(), "conduit-test-"));
  try {
    const repo = new FileReviewResultRepository(dir);
    await repo.save({
      reviewId: "rev-1",
      runId: "r1",
      featureId: "001",
      decision: "approved",
      findings: [{ severity: "info", message: "looks good" }],
      evidencePaths: ["src/index.ts"],
      followUp: undefined,
      reviewedAt: "2026-01-01",
    });

    const result = await repo.load("r1");
    assert.ok(result);
    assert.equal(result!.decision, "approved");
    assert.equal(result!.findings.length, 1);
    assert.equal(result!.evidencePaths[0], "src/index.ts");

    const missing = await repo.load("r2");
    assert.equal(missing, undefined);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

// Process registry cancellation

test("createRunProcessRegistry cancel sends SIGTERM to registered processes", async () => {
  const registry = createRunProcessRegistry();
  let killCalled = false;
  const mockProcess = {
    exitCode: null,
    killed: false,
    kill(_signal: string) {
      killCalled = true;
      return true;
    },
  } as unknown as import("node:child_process").ChildProcess;
  const abortController = new AbortController();

  registry.register({
    runId: "r1",
    roleId: "backend",
    process: mockProcess,
    abortController,
  });
  const cancelled = registry.cancel("r1");
  assert.equal(cancelled, true);
  assert.equal(killCalled, true);
  assert.equal(abortController.signal.aborted, true);
});

test("createRunProcessRegistry getByRun returns all entries for a run", () => {
  const registry = createRunProcessRegistry();
  const mockProcess = {
    exitCode: null,
    killed: false,
    kill() {
      return true;
    },
  } as unknown as import("node:child_process").ChildProcess;
  const ac1 = new AbortController();
  const ac2 = new AbortController();

  registry.register({
    runId: "r1",
    roleId: "backend",
    process: mockProcess,
    abortController: ac1,
  });
  registry.register({
    runId: "r1",
    roleId: "frontend",
    process: mockProcess,
    abortController: ac2,
  });
  registry.register({
    runId: "r2",
    roleId: "qa",
    process: mockProcess,
    abortController: new AbortController(),
  });

  const r1Entries = registry.getByRun("r1");
  assert.equal(r1Entries.length, 2);
});

test("cancelRun handler uses process registry to cancel and append events", async () => {
  const repo = new InMemoryRunEventRepository();
  const registry = createRunProcessRegistry();
  const handler = createCancelRunHandler(repo, registry);

  const result = await handler({ type: "cancelRun", runId: "r1" });
  assert.equal(result.success, true);
  if (result.success) assert.equal(result.data.cancelled, true);

  const events = await repo.loadByRun("r1");
  assert.equal(events.length, 1);
  const payload = events[0]!.payload as LifecyclePayload;
  assert.equal(payload.state, "cancelled");
});

// Event presentation helpers

test("deriveRolePresentation returns correct state for lifecycle events", () => {
  const events: RunnerEvent[] = [
    {
      type: "lifecycle",
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "lifecycle", state: "starting" },
    },
    {
      type: "activity",
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "activity", message: "coding" },
    },
    {
      type: "lifecycle",
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "lifecycle", state: "completed" },
    },
  ];
  const presentation = deriveRolePresentation(events, "be");
  assert.equal(presentation.state, "completed");
  assert.equal(presentation.roleId, "be");
  assert.equal(presentation.eventCount, 3);
});

test("deriveRolePresentation detects unavailable runners", () => {
  const events: RunnerEvent[] = [
    {
      type: "lifecycle",
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: {
        kind: "lifecycle",
        state: "unavailable",
        message: "not found",
      },
    },
  ];
  const presentation = deriveRolePresentation(events, "be");
  assert.equal(presentation.state, "failed");
  assert.equal(presentation.isUnavailable, true);
});

test("deriveRolePresentation shows last activity message when running", () => {
  const events: RunnerEvent[] = [
    {
      type: "lifecycle",
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "lifecycle", state: "running" },
    },
    {
      type: "activity",
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "activity", message: "reading files" },
    },
  ];
  const presentation = deriveRolePresentation(events, "be");
  assert.equal(presentation.state, "working");
  assert.equal(presentation.message, "reading files");
});

test("extractFileDiff extracts diff section for a specific file", () => {
  const patch = `diff --git a/src/index.ts b/src/index.ts
--- a/src/index.ts
+++ b/src/index.ts
@@ -1 +1 @@
-old
+new
diff --git a/src/utils.ts b/src/utils.ts
--- a/src/utils.ts
+++ b/src/utils.ts
@@ -1 +1 @@
-old utils
+new utils`;

  const indexDiff = extractFileDiff(patch, "src/index.ts");
  assert.ok(indexDiff);
  assert.ok(indexDiff.includes("src/index.ts"));
  assert.ok(indexDiff.includes("-old"));
  assert.ok(indexDiff.includes("+new"));

  const utilsDiff = extractFileDiff(patch, "src/utils.ts");
  assert.ok(utilsDiff);
  assert.ok(utilsDiff.includes("src/utils.ts"));

  const missingDiff = extractFileDiff(patch, "src/missing.ts");
  assert.equal(missingDiff, undefined);
});

// CLI backward compatibility

test("commandForRole builds correct args for all runners", async () => {
  const { commandForRole } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  assert.deepEqual(commandForRole({ runner: "opencode" }, "/tmp/p.md"), [
    "opencode",
    ["run", "Read /tmp/p.md and perform only your assigned task."],
  ]);
  assert.deepEqual(commandForRole({ runner: "codex" }, "/tmp/p.md"), [
    "codex",
    ["exec", "Read /tmp/p.md and perform only your assigned task."],
  ]);
  assert.deepEqual(commandForRole({ runner: "pi" }, "/tmp/p.md"), [
    "pi",
    ["-p", "Read /tmp/p.md and perform only your assigned task."],
  ]);
  assert.deepEqual(commandForRole({ runner: "kilo" }, "/tmp/p.md"), [
    "kilo",
    ["run", "Read /tmp/p.md and perform only your assigned task."],
  ]);
});

test("WorktreeDiffReader reports untracked agent-created files", async () => {
  const { WorktreeDiffReader } =
    await import("../src/domains/runs/repositories/worktree-diff-reader.js");
  const dir = await mkdtemp(path.join(tmpdir(), "conduit-diff-"));
  try {
    const { spawnSync } = await import("node:child_process");
    const { writeFile } = await import("node:fs/promises");
    spawnSync("git", ["-C", dir, "init"], { encoding: "utf8" });
    spawnSync("git", ["-C", dir, "config", "user.email", "test@example.com"], {
      encoding: "utf8",
    });
    spawnSync("git", ["-C", dir, "config", "user.name", "Test"], {
      encoding: "utf8",
    });
    await writeFile(path.join(dir, "tracked.txt"), "base\n");
    spawnSync("git", ["-C", dir, "add", "tracked.txt"], { encoding: "utf8" });
    spawnSync("git", ["-C", dir, "commit", "-m", "init"], {
      encoding: "utf8",
    });
    await writeFile(path.join(dir, "agent-output.txt"), "created\n");

    const result = new WorktreeDiffReader().readDiff(dir);

    assert.deepEqual(
      result.changedFiles.map((file) => file.path),
      ["agent-output.txt"],
    );
    assert.ok(result.diff?.includes("agent-output.txt"));
    assert.ok(extractFileDiff(result.diff ?? "", "agent-output.txt"));
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("executeRun persists role worktrees before agent completion and emits flow completion", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { mkdir } = await import("node:fs/promises");
  const { execFileSync } = await import("node:child_process");
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-run-"));
  const previousPath = process.env.PATH;
  try {
    execFileSync("git", ["-C", projectRoot, "init"], { encoding: "utf8" });
    execFileSync(
      "git",
      ["-C", projectRoot, "config", "user.email", "test@example.com"],
      { encoding: "utf8" },
    );
    execFileSync("git", ["-C", projectRoot, "config", "user.name", "Test"], {
      encoding: "utf8",
    });
    await import("node:fs/promises").then(({ writeFile }) =>
      writeFile(path.join(projectRoot, "README.md"), "base\n"),
    );
    execFileSync("git", ["-C", projectRoot, "add", "README.md"], {
      encoding: "utf8",
    });
    execFileSync(
      "git",
      ["-C", projectRoot, "-c", "commit.gpgSign=false", "commit", "-m", "init"],
      { encoding: "utf8" },
    );
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-1");
    await mkdir(runDir, { recursive: true });
    const binDir = path.join(projectRoot, "bin");
    await mkdir(binDir, { recursive: true });
    await import("node:fs/promises").then(({ writeFile, chmod }) =>
      writeFile(
        path.join(binDir, "codex"),
        "#!/bin/sh\nprintf 'created\n' > agent-output.txt\nsleep 0.25\n",
      ).then(() => chmod(path.join(binDir, "codex"), 0o755)),
    );
    process.env.PATH = `${binDir}:${previousPath ?? ""}`;
    const eventRepository = new InMemoryRunEventRepository();
    const run: Run = {
      id: "run-1",
      featureId: "001",
      status: "planned" as const,
      createdAt: new Date().toISOString(),
      roles: [
        {
          name: "backend",
          runner: "codex",
          readOnly: false,
          owns: ["src"],
          dependsOn: [],
          promptFile: path.join(runDir, "backend.md"),
          prompt: "prompt",
          command: "codex",
          args: [],
          skillSource: "test",
          status: "planned" as const,
        },
        {
          name: "reviewer",
          runner: "node",
          readOnly: true,
          owns: [],
          dependsOn: ["backend"],
          promptFile: path.join(runDir, "reviewer.md"),
          prompt: "review",
          command: process.execPath,
          args: ["-e", "process.exit(0)"],
          skillSource: "test",
          status: "planned" as const,
        },
      ],
    };

    const executing = executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      eventRepository,
    });

    let persistedWorktree = "";
    for (let attempt = 0; attempt < 20; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 25));
      persistedWorktree = run.roles[0]?.worktree ?? "";
      if (persistedWorktree) break;
    }
    const results = await executing;
    assert.ok(persistedWorktree);
    assert.equal(
      results.every((result) => result.status === "completed"),
      true,
    );
    const events = await eventRepository.loadByRun("run-1");
    assert.ok(
      events.some(
        (event) =>
          event.roleId === "system" &&
          event.payload.kind === "activity" &&
          event.payload.message.includes("Flow finished"),
      ),
    );
  } finally {
    process.env.PATH = previousPath;
    await rm(projectRoot, { recursive: true, force: true });
    await rm(path.join(tmpdir(), ".conduit-worktrees"), {
      recursive: true,
      force: true,
    });
  }
});

test("executeRun follows configured role dependency groups", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { mkdir, readFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-flow-"));
  try {
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-flow");
    await mkdir(runDir, { recursive: true });
    const marker = (name: string) => path.join(projectRoot, `${name}.done`);
    const script = (name: string, dependencies: string[]) => `
      const fs = require("fs");
      const missing = ${JSON.stringify(dependencies)}.filter(
        (dependency) => !fs.existsSync(${JSON.stringify(projectRoot)} + "/" + dependency + ".done"),
      );
      if (missing.length) {
        console.error("missing dependencies: " + missing.join(","));
        process.exit(1);
      }
      fs.writeFileSync(${JSON.stringify(projectRoot)} + "/" + ${JSON.stringify(name)} + ".done", "done");
    `;
    const role = (
      name: string,
      dependsOn: string[] = [],
    ): Run["roles"][number] => ({
      name,
      runner: "node",
      readOnly: true,
      owns: [],
      dependsOn,
      promptFile: path.join(runDir, `${name}.md`),
      prompt: name,
      command: process.execPath,
      args: ["-e", script(name, dependsOn)],
      skillSource: "test",
      status: "planned" as const,
    });
    const run: Run = {
      id: "run-flow",
      featureId: "001",
      status: "planned" as const,
      createdAt: new Date().toISOString(),
      roles: [
        role("frontend"),
        role("backend"),
        role("qa", ["frontend", "backend"]),
        role("documentation", ["frontend", "backend"]),
        role("reviewer", ["qa", "documentation"]),
      ],
    };

    const results = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
    });

    assert.deepEqual(
      results.map((result) => result.role),
      ["frontend", "backend", "qa", "documentation", "reviewer"],
    );
    await Promise.all(
      ["frontend", "backend", "qa", "documentation", "reviewer"].map(
        async (name) =>
          assert.equal(await readFile(marker(name), "utf8"), "done"),
      ),
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
