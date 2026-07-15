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
import { RunnerEventProvenance } from "../src/domains/runs/enums/runner-event-provenance.js";
import { createAgentAssignmentV1 } from "../src/domains/runs/factories/agent-assignment-factory.js";
import { roleKindForRole } from "../src/domains/runs/validation/agent-semantic-validator.js";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

function assignmentFor(
  runId: string,
  name: string,
  ownedPaths: readonly string[],
) {
  return createAgentAssignmentV1({
    assignmentId: `${runId}:${name}`,
    role: name,
    roleKind: roleKindForRole(name),
    objective: `Complete the ${name} test assignment.`,
    ownedPaths,
    contextReferences: [],
    acceptanceCriteria: ["Return a valid AgentResponseV1."],
    contracts: ["specs"],
  });
}

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
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "lifecycle", state: "starting" },
    },
    {
      type: "activity",
      provenance: RunnerEventProvenance.RunnerReported,
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "activity", message: "coding" },
    },
    {
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
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
      provenance: RunnerEventProvenance.ConduitObserved,
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

test("deriveRolePresentation shows a bounded activity phase when running", () => {
  const events: RunnerEvent[] = [
    {
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: { kind: "lifecycle", state: "running" },
    },
    {
      type: "activity",
      provenance: RunnerEventProvenance.RunnerReported,
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: {
        kind: "activity",
        message:
          "A very long reasoning summary that must remain in event details.",
      },
    },
  ];
  const presentation = deriveRolePresentation(events, "be");
  assert.equal(presentation.state, "working");
  assert.equal(presentation.message, "thinking");
});

test("terminal lifecycle state wins over intermediate response activity", () => {
  const events: RunnerEvent[] = [
    {
      type: "activity",
      provenance: RunnerEventProvenance.AgentClaimed,
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: {
        kind: "activity",
        message: "backend: final AgentResponseV1 received",
      },
    },
    {
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: "r",
      roleId: "be",
      timestamp: "",
      payload: {
        kind: "lifecycle",
        state: "failed",
        message: "backend: failed",
      },
    },
  ];

  const presentation = deriveRolePresentation(events, "be");
  assert.equal(presentation.state, "failed");
  assert.equal(presentation.message, "failed");
});

test("activity header uses phases while the event list keeps bounded details", async () => {
  const { activityPhaseForEvent, formatEventDescription } =
    await import("../src/tui/helpers/event-presentation.js");
  const readingEvent: RunnerEvent = {
    type: "tool-call",
    provenance: RunnerEventProvenance.RunnerReported,
    runId: "r",
    roleId: "be",
    timestamp: "",
    payload: {
      kind: "tool-call",
      tool: "shell",
      args: "sed -n '1,120p' src/server.ts",
    },
  };
  const thoughtEvent: RunnerEvent = {
    type: "activity",
    provenance: RunnerEventProvenance.RunnerReported,
    runId: "r",
    roleId: "be",
    timestamp: "",
    payload: {
      kind: "activity",
      message: "This detailed reasoning text should not occupy the header.",
    },
  };

  assert.equal(activityPhaseForEvent(readingEvent), "reading");
  assert.equal(
    formatEventDescription(readingEvent),
    "Called shell(sed -n '1,120p' src/server.ts)",
  );
  assert.equal(
    formatEventDescription(thoughtEvent),
    "This detailed reasoning text should not occupy the header.",
  );
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
    [
      "run",
      "--format",
      "json",
      "Read /tmp/p.md and perform only your assigned task.",
    ],
  ]);
  assert.deepEqual(commandForRole({ runner: "codex" }, "/tmp/p.md"), [
    "codex",
    ["exec", "--json", "Read /tmp/p.md and perform only your assigned task."],
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
    const { mkdir, symlink, writeFile } = await import("node:fs/promises");
    spawnSync("git", ["-C", dir, "init"], { encoding: "utf8" });
    spawnSync("git", ["-C", dir, "config", "user.email", "test@example.com"], {
      encoding: "utf8",
    });
    spawnSync("git", ["-C", dir, "config", "user.name", "Test"], {
      encoding: "utf8",
    });
    await writeFile(path.join(dir, "tracked.txt"), "base\n");
    await writeFile(path.join(dir, ".gitignore"), "generated/\n");
    spawnSync("git", ["-C", dir, "add", "tracked.txt", ".gitignore"], {
      encoding: "utf8",
    });
    spawnSync("git", ["-C", dir, "commit", "-m", "init"], {
      encoding: "utf8",
    });
    await writeFile(path.join(dir, "agent-output.txt"), "created\n");
    await mkdir(path.join(dir, ".conduit", "assignments"), {
      recursive: true,
    });
    await writeFile(
      path.join(dir, ".conduit", "assignments", "internal.json"),
      "{}\n",
    );
    await mkdir(path.join(dir, ".conduit", "dependencies", "package"), {
      recursive: true,
    });
    await symlink(
      path.join(dir, ".conduit", "dependencies"),
      path.join(dir, "vendor"),
    );
    await mkdir(path.join(dir, "node_modules", "package"), {
      recursive: true,
    });
    await writeFile(path.join(dir, "node_modules", "package", "index.js"), "");
    await mkdir(path.join(dir, "nested", "node_modules", "package"), {
      recursive: true,
    });
    await writeFile(
      path.join(dir, "nested", "node_modules", "package", "index.js"),
      "",
    );
    for (const generatedDirectory of [
      "dist",
      "coverage",
      "test-results",
      "playwright-report",
    ]) {
      await mkdir(path.join(dir, generatedDirectory), { recursive: true });
      await writeFile(
        path.join(dir, generatedDirectory, "generated.json"),
        "{}\n",
      );
    }
    await writeFile(path.join(dir, "large-output.txt"), "x".repeat(300 * 1024));

    const result = new WorktreeDiffReader().readDiff(dir);

    assert.deepEqual(
      result.changedFiles.map((file) => file.path),
      ["agent-output.txt", "large-output.txt"],
    );
    assert.ok(result.diff?.includes("agent-output.txt"));
    assert.equal(result.diff?.includes("large-output.txt"), false);
    assert.equal(result.diff?.includes("generated.json"), false);
    assert.equal(result.diff?.includes("internal.json"), false);
    assert.ok(extractFileDiff(result.diff ?? "", "agent-output.txt"));
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("WorktreeDiffReader reports files in an unborn repository", async () => {
  const { WorktreeDiffReader } =
    await import("../src/domains/runs/repositories/worktree-diff-reader.js");
  const dir = await mkdtemp(path.join(tmpdir(), "conduit-unborn-diff-"));
  try {
    const { spawnSync } = await import("node:child_process");
    const { writeFile } = await import("node:fs/promises");
    spawnSync("git", ["-C", dir, "init"], { encoding: "utf8" });
    await writeFile(path.join(dir, "package.json"), "{}\n");

    const result = new WorktreeDiffReader().readDiff(dir);

    assert.deepEqual(result.changedFiles, [
      { path: "package.json", additions: 2, deletions: 0 },
    ]);
    assert.ok(result.diff?.includes("package.json"));
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("executeRun retains an unborn repository workspace for diff queries", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { chmod, mkdir, writeFile } = await import("node:fs/promises");
  const { spawnSync } = await import("node:child_process");
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-unborn-run-"));
  const runDir = path.join(projectRoot, ".conduit", "runs", "run-unborn");
  const previousPath = process.env.PATH;
  try {
    spawnSync("git", ["-C", projectRoot, "init"], { encoding: "utf8" });
    await mkdir(runDir, { recursive: true });
    const response = {
      protocolVersion: "1.0",
      status: "completed",
      summary: "Implemented the assigned change.",
      verdict: null,
      artifacts: [
        {
          path: "package.json",
          category: "configuration",
          purpose: "configure the application",
          action: "created",
        },
      ],
      findings: [],
      verification: [
        { operation: "node --check", outcome: "passed", summary: "passed" },
      ],
      decisions: [],
      blockers: [],
      questions: [],
      risks: [],
      evidence: [],
      memoryProposals: [],
      globalPromotionProposals: [],
    };
    const binDir = path.join(projectRoot, "bin");
    await mkdir(binDir, { recursive: true });
    await writeFile(
      path.join(binDir, "codex"),
      `#!/bin/sh
if ! mkdir .conduit/test-agent-lock 2>/dev/null; then
  exit 91
fi
sleep 0.05
rmdir .conduit/test-agent-lock
printf '%s\n' '${JSON.stringify(response)}'
`,
    );
    await chmod(path.join(binDir, "codex"), 0o755);
    process.env.PATH = `${binDir}:${previousPath ?? ""}`;
    const run: Run = {
      id: "run-unborn",
      featureId: "001",
      status: "planned",
      createdAt: new Date().toISOString(),
      stateDirectory: path.join(projectRoot, ".conduit"),
      roles: [
        {
          name: "frontend",
          runner: "codex",
          readOnly: false,
          owns: ["./"],
          dependsOn: [],
          promptFile: path.join(runDir, "frontend-assignment.json"),
          prompt: "prompt",
          command: "codex",
          args: [],
          skillSource: "test",
          status: "planned",
          assignment: assignmentFor("run-unborn", "frontend", ["./"]),
        },
        {
          name: "backend",
          runner: "codex",
          readOnly: false,
          owns: ["./"],
          dependsOn: [],
          promptFile: path.join(runDir, "backend-assignment.json"),
          prompt: "prompt",
          command: "codex",
          args: [],
          skillSource: "test",
          status: "planned",
          assignment: assignmentFor("run-unborn", "backend", ["./"]),
        },
      ],
    };

    let workspaceReadyCalls = 0;
    const results = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      onRoleWorkspaceReady: async () => {
        workspaceReadyCalls += 1;
        assert.equal(
          run.roles.filter((role) => role.worktree === projectRoot).length,
          workspaceReadyCalls,
        );
      },
    });

    assert.equal(
      results.every((result) => result.status === "completed"),
      true,
      JSON.stringify(results),
    );
    assert.equal(
      run.roles.every((role) => role.worktree === projectRoot),
      true,
    );
    assert.equal(workspaceReadyCalls, 2);
  } finally {
    process.env.PATH = previousPath;
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("executeRun persists role worktrees before agent completion and emits flow completion", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { mkdir, readFile } = await import("node:fs/promises");
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
    const hooksDirectory = path.join(projectRoot, ".git", "hooks");
    await import("node:fs/promises").then(async ({ chmod, writeFile }) => {
      const hook = path.join(hooksDirectory, "post-checkout");
      await writeFile(hook, "#!/bin/sh\nexit 91\n");
      await chmod(hook, 0o755);
    });
    await mkdir(path.join(projectRoot, "node_modules"), { recursive: true });
    await mkdir(path.join(projectRoot, "vendor"), { recursive: true });
    await import("node:fs/promises").then(({ writeFile }) =>
      writeFile(path.join(projectRoot, "node_modules", ".sentinel"), "ready"),
    );
    await import("node:fs/promises").then(({ writeFile }) =>
      writeFile(path.join(projectRoot, "vendor", ".sentinel"), "ready"),
    );
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-1");
    await mkdir(runDir, { recursive: true });
    const binDir = path.join(projectRoot, "bin");
    await mkdir(binDir, { recursive: true });
    await import("node:fs/promises").then(({ writeFile, chmod }) =>
      writeFile(
        path.join(binDir, "codex"),
        `#!/bin/sh
mkdir -p src
mkdir -p dist node_modules/.vite/vitest test-results
printf 'created\n' > src/generated.ts
printf 'generated build\n' > dist/index.html
printf '{}\n' > node_modules/.vite/vitest/results.json
printf '{}\n' > test-results/.last-run.json
printf 'export default {}\n' > vitest.config.js
printf '%s\n' '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":null,"artifacts":[{"path":"src/generated.ts","category":"source","purpose":"test","action":"modified"},{"path":"vitest.config.js","category":"configuration","purpose":"test configuration","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}'
sleep 0.25
`,
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
          assignment: assignmentFor("run-1", "backend", ["src"]),
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
          args: [
            "-e",
            "console.log(JSON.stringify(" +
              '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":{"decision":"approved","rationale":"ok"},"artifacts":[],"findings":[],"verification":[],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}' +
              "))",
          ],
          skillSource: "test",
          status: "planned" as const,
          assignment: assignmentFor("run-1", "reviewer", []),
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
      await readFile(
        path.join(persistedWorktree, "node_modules", ".sentinel"),
        "utf8",
      ),
      "ready",
    );
    assert.equal(
      await readFile(
        path.join(persistedWorktree, "vendor", ".sentinel"),
        "utf8",
      ),
      "ready",
    );
    assert.equal(
      results.every((result) => result.status === "completed"),
      true,
      JSON.stringify(results),
    );
    assert.equal(results[0]?.resultRecord?.assignmentId, "run-1:backend");
    assert.ok(
      results[0]?.resultRecord?.observedChangedFiles.includes(
        "src/generated.ts",
      ),
    );
    assert.equal(
      results[0]?.resultRecord?.observedChangedFiles.some(
        (file) =>
          file.startsWith("dist/") ||
          file.startsWith("node_modules/") ||
          file.startsWith("test-results/"),
      ),
      false,
    );
    assert.deepEqual(
      results[0]?.resultRecord?.ownershipWarnings?.map(
        (warning) => warning.message,
      ),
      [
        "reported modification outside assigned ownership: vitest.config.js",
        "Conduit observed a change outside assigned ownership: vitest.config.js",
      ],
    );
    assert.ok(
      results[0]?.resultRecord?.conduitObservedEvents.every(
        (event) => event.provenance === RunnerEventProvenance.ConduitObserved,
      ),
    );
    assert.equal(
      JSON.parse(
        await import("node:fs/promises").then(({ readFile }) =>
          readFile(path.join(runDir, "backend-result.json"), "utf8"),
        ),
      ).recordVersion,
      "1.0",
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
      const review = '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":{"decision":"approved","rationale":"ok"},"artifacts":[],"findings":[],"verification":[],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
      const impl = '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":null,"artifacts":[{"path":"src/generated.ts","category":"source","purpose":"test fixture evidence","action":"inspected"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
      const qa = impl;
      const docs = impl;
      const content = ${JSON.stringify(name)}.includes("reviewer") ? review : impl;
      console.log(content);
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
      assignment: assignmentFor("run-flow", name, []),
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

test("zero-exit invalid and incomplete responses block dependents", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { mkdir, readFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-protocol-"));
  try {
    const cases = [
      { name: "invalid", output: "not-json" },
      {
        name: "partial",
        output: JSON.stringify({
          protocolVersion: "1.0",
          status: "partial",
          summary: "partial",
          verdict: null,
          artifacts: [],
          findings: [],
          verification: [],
          decisions: [],
          blockers: [],
          questions: [],
          risks: [],
          evidence: [],
          memoryProposals: [],
          globalPromotionProposals: [],
        }),
      },
      {
        name: "blocked",
        output: JSON.stringify({
          protocolVersion: "1.0",
          status: "blocked",
          summary: "blocked",
          verdict: null,
          artifacts: [],
          findings: [],
          verification: [],
          decisions: [],
          blockers: [
            {
              blocker: "missing input",
              impact: "cannot continue",
              minimumUnblocker: "provide input",
            },
          ],
          questions: [],
          risks: [],
          evidence: [],
          memoryProposals: [],
          globalPromotionProposals: [],
        }),
      },
      {
        name: "needs-input",
        output: JSON.stringify({
          protocolVersion: "1.0",
          status: "needs_input",
          summary: "question",
          verdict: null,
          artifacts: [],
          findings: [],
          verification: [],
          decisions: [],
          blockers: [],
          questions: [
            {
              question: "Which option?",
              whyItMatters: "Changes behavior.",
              context: "No decision exists.",
              options: ["A", "B"],
              smallestUnblocker: "Choose one.",
            },
          ],
          risks: [],
          evidence: [],
          memoryProposals: [],
          globalPromotionProposals: [],
        }),
      },
    ];

    for (const scenario of cases) {
      const runId = `run-${scenario.name}`;
      const runDir = path.join(projectRoot, ".conduit", "runs", runId);
      const marker = path.join(projectRoot, `${scenario.name}-dependent.txt`);
      await mkdir(runDir, { recursive: true });
      const run: Run = {
        id: runId,
        featureId: "007",
        status: "planned",
        createdAt: new Date().toISOString(),
        roles: [
          {
            name: "backend",
            runner: "node",
            readOnly: true,
            owns: ["src"],
            dependsOn: [],
            promptFile: path.join(runDir, "backend-assignment.json"),
            prompt: "{}",
            command: process.execPath,
            args: ["-e", `console.log(${JSON.stringify(scenario.output)})`],
            skillSource: "test",
            status: "planned",
            assignment: assignmentFor(runId, "backend", ["src"]),
          },
          {
            name: "qa",
            runner: "node",
            readOnly: true,
            owns: [],
            dependsOn: ["backend"],
            promptFile: path.join(runDir, "qa-assignment.json"),
            prompt: "{}",
            command: process.execPath,
            args: [
              "-e",
              `require("fs").writeFileSync(${JSON.stringify(marker)}, "ran")`,
            ],
            skillSource: "test",
            status: "planned",
            assignment: assignmentFor(runId, "qa", []),
          },
        ],
      };

      const results = await executeRun({
        projectRoot,
        run,
        runDir,
        dryRun: false,
      });
      assert.deepEqual(
        results.map((result) => result.status),
        ["failed", "failed"],
        scenario.name,
      );
      assert.equal(
        await readFile(marker, "utf8").catch(() => undefined),
        undefined,
        scenario.name,
      );
    }
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("expired terminal worktrees are removed through recorded lifecycle metadata", async () => {
  const { execFileSync } = await import("node:child_process");
  const { mkdir, readFile, writeFile } = await import("node:fs/promises");
  const { FileWorktreeLifecycleRepository } =
    await import("../src/domains/runs/repositories/file-worktree-lifecycle-repository.js");
  const { cleanupExpiredWorktrees } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-retention-"));
  const stateDirectory = path.join(projectRoot, ".state");
  const worktree = path.join(
    path.dirname(projectRoot),
    `${path.basename(projectRoot)}-worker`,
  );
  try {
    execFileSync("git", ["-C", projectRoot, "init"]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "config",
      "user.email",
      "test@example.com",
    ]);
    execFileSync("git", ["-C", projectRoot, "config", "user.name", "Test"]);
    await writeFile(path.join(projectRoot, "README.md"), "base\n");
    execFileSync("git", ["-C", projectRoot, "add", "README.md"]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "-m",
      "init",
    ]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "worktree",
      "add",
      "-b",
      "worker",
      worktree,
      "HEAD",
    ]);
    await mkdir(stateDirectory, { recursive: true });
    const repository = new FileWorktreeLifecycleRepository(stateDirectory);
    await repository.save({
      runId: "expired",
      status: "completed",
      worktrees: [worktree],
      completedAt: "2020-01-01T00:00:00.000Z",
    });

    await cleanupExpiredWorktrees(projectRoot, stateDirectory, 0);

    assert.equal(
      await readFile(path.join(worktree, "README.md"), "utf8").catch(
        () => undefined,
      ),
      undefined,
    );
    assert.equal(
      await repository
        .listExpired(new Date())
        .then((records) => records.length),
      0,
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(worktree, { recursive: true, force: true });
  }
});

test("expired raw run diagnostics are removed while validated results remain", async () => {
  const { mkdir, readFile, writeFile } = await import("node:fs/promises");
  const { cleanupExpiredRunDiagnostics } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const stateDirectory = await mkdtemp(
    path.join(tmpdir(), "conduit-diagnostics-"),
  );
  const runDirectory = path.join(stateDirectory, "runs", "old-run");
  try {
    await mkdir(runDirectory, { recursive: true });
    await writeFile(
      path.join(runDirectory, "terminal.json"),
      '{"status":"completed","completedAt":"2020-01-01T00:00:00.000Z"}',
    );
    await writeFile(path.join(runDirectory, "backend.log"), "raw");
    await writeFile(
      path.join(runDirectory, "backend-agent-response.json"),
      "{}",
    );
    await writeFile(path.join(runDirectory, "backend-result.json"), "{}");

    await cleanupExpiredRunDiagnostics(stateDirectory, 0);

    assert.equal(
      await readFile(path.join(runDirectory, "backend.log"), "utf8").catch(
        () => undefined,
      ),
      undefined,
    );
    assert.equal(
      await readFile(path.join(runDirectory, "backend-result.json"), "utf8"),
      "{}",
    );
  } finally {
    await rm(stateDirectory, { recursive: true, force: true });
  }
});
