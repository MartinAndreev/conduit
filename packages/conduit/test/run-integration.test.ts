import { test } from "bun:test";
import { createHash } from "node:crypto";
import assert from "node:assert/strict";
import { FileRunEventRepository } from "../src/domains/runs/repositories/file-run-event-repository.js";
import { FileReviewResultRepository } from "../src/domains/runs/repositories/file-review-result-repository.js";
import { createRunProcessRegistry } from "../src/domains/runs/repositories/run-process-registry.js";
import { createCancelRunHandler } from "../src/domains/runs/handlers/cancel-run-handler.js";
import { InMemoryRunEventRepository } from "../src/domains/runs/repositories/in-memory-run-event-repository.js";
import {
  canonicalMonitorRoleId,
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
import { commandCommunicationProvider } from "./helpers/command-communication-provider.js";

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

test("monitor folds retry transcript roles into configured roles", () => {
  const configured = ["documentation", "reviewer"];
  assert.equal(
    canonicalMonitorRoleId("documentation-resume-4", configured),
    "documentation",
  );
  assert.equal(
    canonicalMonitorRoleId("reviewer-auto-retry-1", configured),
    "reviewer",
  );
  assert.equal(canonicalMonitorRoleId("frontend", configured), "frontend");
});

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
    [
      "--mode",
      "json",
      "--print",
      "--no-extensions",
      "--no-skills",
      "--no-prompt-templates",
      "--no-context-files",
      "--no-session",
      "Read /tmp/p.md and perform only your assigned task.",
    ],
  ]);
  assert.deepEqual(commandForRole({ runner: "kilo" }, "/tmp/p.md"), [
    "kilo",
    [
      "run",
      "--pure",
      "--format",
      "json",
      "Read /tmp/p.md and perform only your assigned task.",
    ],
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
    spawnSync(
      "git",
      ["-C", dir, "-c", "commit.gpgSign=false", "commit", "-m", "init"],
      {
        encoding: "utf8",
      },
    );
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
}, 15_000);

test("WorktreeDiffReader retains committed changes and full-file context from a baseline", async () => {
  const { WorktreeDiffReader } =
    await import("../src/domains/runs/repositories/worktree-diff-reader.js");
  const dir = await mkdtemp(path.join(tmpdir(), "conduit-baseline-diff-"));
  try {
    const { spawnSync } = await import("node:child_process");
    const { writeFile } = await import("node:fs/promises");
    spawnSync("git", ["-C", dir, "init"], { encoding: "utf8" });
    spawnSync("git", ["-C", dir, "config", "user.email", "test@example.com"]);
    spawnSync("git", ["-C", dir, "config", "user.name", "Test"]);
    const original = Array.from(
      { length: 30 },
      (_, index) => `line ${index + 1}`,
    );
    await writeFile(path.join(dir, "long.txt"), `${original.join("\n")}\n`);
    spawnSync("git", ["-C", dir, "add", "long.txt"]);
    spawnSync("git", [
      "-C",
      dir,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "-m",
      "base",
    ]);
    const baseline = spawnSync("git", ["-C", dir, "rev-parse", "HEAD"], {
      encoding: "utf8",
    }).stdout.trim();
    original[14] = "line 15 changed";
    await writeFile(path.join(dir, "long.txt"), `${original.join("\n")}\n`);
    spawnSync("git", ["-C", dir, "add", "long.txt"]);
    spawnSync("git", [
      "-C",
      dir,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "-m",
      "change",
    ]);

    const result = new WorktreeDiffReader().readDiff(dir, baseline);

    assert.deepEqual(result.changedFiles, [
      { path: "long.txt", additions: 1, deletions: 1 },
    ]);
    assert.match(result.diff ?? "", /line 1\n/);
    assert.match(result.diff ?? "", /line 30$/);
    assert.match(result.diff ?? "", /line 15 changed/);
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

test("executeRun refuses an unborn repository rather than exposing project state", async () => {
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
if [ "$1" = "--version" ]; then echo "codex-cli 0.144.4"; exit 0; fi
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
      results.every(
        (result) =>
          result.status === "failed" &&
          result.error?.includes("requires a committed Git HEAD"),
      ),
      true,
      JSON.stringify(results),
    );
    assert.equal(
      run.roles.every((role) => role.worktree === undefined),
      true,
    );
    assert.equal(workspaceReadyCalls, 0);
  } finally {
    process.env.PATH = previousPath;
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("shared read-only research runs in an unborn project without creating a worktree", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { mkdir, readFile, writeFile } = await import("node:fs/promises");
  const { execFileSync } = await import("node:child_process");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-research-unborn-"),
  );
  try {
    execFileSync("git", ["-C", projectRoot, "init"]);
    await writeFile(path.join(projectRoot, "story.md"), "research me\n");
    const runDir = path.join(projectRoot, ".conduit", "runs", "research");
    await mkdir(runDir, { recursive: true });
    const response =
      '{"protocolVersion":"1.0","status":"completed","summary":"researched","verdict":null,"artifacts":[],"findings":[],"verification":[],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[{"kind":"path","reference":"story.md"}],"memoryProposals":[],"globalPromotionProposals":[]}';
    const run: Run = {
      id: "research",
      featureId: "001",
      status: "planned",
      createdAt: new Date().toISOString(),
      roles: [
        {
          name: "researcher",
          runner: "codex",
          readOnly: true,
          owns: [],
          dependsOn: [],
          promptFile: "",
          prompt: "",
          command: "",
          args: [],
          skillSource: "test",
          status: "planned",
          worktree: projectRoot,
          assignment: assignmentFor("research", "researcher", []),
        },
      ],
    };
    const results = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      sharedReadOnlyWorkspace: true,
      communicationProviders: [
        commandCommunicationProvider({
          researcher: {
            command: process.execPath,
            args: ["-e", `console.log(${JSON.stringify(response)})`],
          },
        }),
      ],
    });
    assert.equal(results[0]?.status, "completed");
    assert.equal(run.status, "completed");
    assert.equal(run.roles[0]?.worktree, projectRoot);
    assert.equal(run.roles[0]?.worktreeHead, undefined);
    assert.equal(
      await readFile(path.join(projectRoot, "story.md"), "utf8"),
      "research me\n",
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("executeRun persists role worktrees before agent completion and emits flow completion", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { lstat, mkdir, readFile } = await import("node:fs/promises");
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
    await import("node:fs/promises").then(async ({ mkdir, writeFile }) => {
      await writeFile(path.join(projectRoot, "README.md"), "base\n");
      await writeFile(
        path.join(projectRoot, ".gitignore"),
        "node_modules/\nvendor/\n",
      );
      await mkdir(path.join(projectRoot, ".conduit"), { recursive: true });
      await writeFile(
        path.join(projectRoot, ".conduit", ".gitignore"),
        "runs/\nstate.db\n",
      );
    });
    execFileSync(
      "git",
      [
        "-C",
        projectRoot,
        "add",
        "README.md",
        ".gitignore",
        ".conduit/.gitignore",
      ],
      { encoding: "utf8" },
    );
    execFileSync(
      "git",
      ["-C", projectRoot, "-c", "commit.gpgSign=false", "commit", "-m", "init"],
      { encoding: "utf8" },
    );
    const hooksDirectory = path.join(projectRoot, ".git", "hooks");
    const hookMarker = path.join(projectRoot, "hook-ran");
    await import("node:fs/promises").then(async ({ chmod, writeFile }) => {
      const postCheckout = path.join(hooksDirectory, "post-checkout");
      await writeFile(postCheckout, "#!/bin/sh\nexit 91\n");
      await chmod(postCheckout, 0o755);
      const postMerge = path.join(hooksDirectory, "post-merge");
      await writeFile(
        postMerge,
        `#!/bin/sh\nprintf ran > ${JSON.stringify(hookMarker)}\nexit 92\n`,
      );
      await chmod(postMerge, 0o755);
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
if [ "$1" = "--version" ]; then echo "codex-cli 0.144.4"; exit 0; fi
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
    execFileSync("git", ["-C", projectRoot, "add", "bin/codex"]);
    execFileSync("git", [
      "-c",
      "core.hooksPath=/dev/null",
      "-c",
      "commit.gpgSign=false",
      "-C",
      projectRoot,
      "commit",
      "-m",
      "test: add fake runner",
    ]);
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
          runner: "codex",
          readOnly: true,
          owns: [],
          dependsOn: ["backend"],
          promptFile: path.join(runDir, "reviewer.md"),
          prompt: "review",
          command: process.execPath,
          args: [
            "-e",
            "require('fs').readFileSync('node_modules/.sentinel','utf8');console.log(JSON.stringify(" +
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
      communicationProviders: [
        commandCommunicationProvider(
          Object.fromEntries(
            run.roles.map((role) => [
              role.name,
              { command: role.command, args: role.args },
            ]),
          ),
        ),
      ],
    });

    let persistedWorktree = "";
    for (let attempt = 0; attempt < 20; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 25));
      persistedWorktree = run.roles[0]?.worktree ?? "";
      if (persistedWorktree) break;
    }
    await rm(path.join(projectRoot, "node_modules"), {
      recursive: true,
      force: true,
    });
    await rm(path.join(projectRoot, "vendor"), {
      recursive: true,
      force: true,
    });
    const results = await executing;
    assert.ok(persistedWorktree);
    assert.equal(
      await readFile(
        path.join(persistedWorktree, "node_modules", ".sentinel"),
        "utf8",
      ),
      "ready",
    );
    const dependencyTree = await lstat(
      path.join(persistedWorktree, "node_modules"),
    );
    assert.equal(dependencyTree.isDirectory(), true);
    assert.equal(dependencyTree.isSymbolicLink(), false);
    assert.equal(
      await readFile(
        path.join(persistedWorktree, "vendor", ".sentinel"),
        "utf8",
      ),
      "ready",
    );
    await assert.rejects(
      readFile(path.join(persistedWorktree, ".conduit", ".gitignore")),
    );
    assert.doesNotMatch(
      execFileSync("git", ["-C", persistedWorktree, "status", "--short"], {
        encoding: "utf8",
      }),
      /\.conduit/,
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
      true,
    );
    assert.deepEqual(
      results[0]?.resultRecord?.ownershipWarnings?.map(
        (warning) => warning.message,
      ),
      [
        "reported modification outside assigned ownership: vitest.config.js",
        "Conduit observed a change outside assigned ownership: dist/index.html",
        "Conduit observed a change outside assigned ownership: test-results/.last-run.json",
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
    await assert.rejects(readFile(hookMarker));
    const events = await eventRepository.loadByRun("run-1");
    assert.ok(
      events.some(
        (event) =>
          event.roleId === "system" &&
          event.payload.kind === "activity" &&
          event.payload.message.includes("Flow finished"),
      ),
      JSON.stringify(events.slice(-8)),
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
    const { execFileSync } = await import("node:child_process");
    const { writeFile } = await import("node:fs/promises");
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
      if (${JSON.stringify(name)} === "frontend") {
        fs.mkdirSync("src", { recursive: true });
        fs.writeFileSync("src/generated.ts", "export const integrated = true;\\n");
      }
      if (${JSON.stringify(dependencies)}.includes("frontend") && !fs.existsSync("src/generated.ts")) {
        console.error("frontend artifacts were not integrated");
        process.exit(2);
      }
      fs.writeFileSync(${JSON.stringify(projectRoot)} + "/" + ${JSON.stringify(name)} + ".done", "done");
      const review = '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":{"decision":"approved","rationale":"ok"},"artifacts":[],"findings":[],"verification":[],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
      const impl = '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":null,"artifacts":[{"path":"src/generated.ts","category":"source","purpose":"test fixture evidence","action":"inspected"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
      const frontend = '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":null,"artifacts":[{"path":"src/generated.ts","category":"source","purpose":"dependency artifact","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
      const content = ${JSON.stringify(name)} === "reviewer" ? review : ${JSON.stringify(name)} === "frontend" ? frontend : impl;
      console.log(content);
    `;
    const role = (
      name: string,
      dependsOn: string[] = [],
    ): Run["roles"][number] => ({
      name,
      runner: "codex",
      readOnly: name !== "frontend",
      owns: name === "frontend" ? ["src"] : [],
      dependsOn,
      promptFile: path.join(runDir, `${name}.md`),
      prompt: name,
      command: process.execPath,
      args: ["-e", script(name, dependsOn)],
      skillSource: "test",
      status: "planned" as const,
      assignment: assignmentFor(
        "run-flow",
        name,
        name === "frontend" ? ["src"] : [],
      ),
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
      communicationProviders: [
        commandCommunicationProvider(
          Object.fromEntries(
            run.roles.map((role) => [
              role.name,
              { command: role.command, args: role.args },
            ]),
          ),
        ),
      ],
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

test("reviewer needs_changes routes a bounded correction and requires approval", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, readFile, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-review-loop-"),
  );
  const assignmentIds: string[] = [];
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
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-review");
    await mkdir(runDir, { recursive: true });
    const response = (value: object) =>
      JSON.stringify({
        protocolVersion: "1.0",
        status: "completed",
        summary: "ok",
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
        ...value,
      });
    const backendInitial = response({
      verdict: null,
      artifacts: [
        {
          path: "src/app.ts",
          category: "source",
          purpose: "implementation",
          action: "created",
        },
      ],
      verification: [{ operation: "test", outcome: "passed", summary: "ok" }],
    });
    const backendCorrection = response({
      verdict: null,
      artifacts: [
        {
          path: "src/app.ts",
          category: "source",
          purpose: "review correction",
          action: "modified",
        },
      ],
      verification: [
        { operation: "test", outcome: "passed", summary: "fixed" },
      ],
    });
    const needsChanges = response({
      verdict: {
        decision: "needs_changes",
        rationale: "Fix the implementation.",
      },
      findings: [
        {
          severity: "error",
          category: "correctness",
          message: "Use the corrected value.",
          path: "src/app.ts",
          line: 1,
          evidence: ["src/app.ts:1"],
        },
      ],
    });
    const approved = response({
      verdict: { decision: "approved", rationale: "Correction verified." },
    });
    const roles: Run["roles"] = [
      {
        name: "backend",
        runner: "codex",
        readOnly: false,
        owns: ["src"],
        dependsOn: [],
        promptFile: path.join(runDir, "backend.md"),
        prompt: "backend",
        command: process.execPath,
        args: [],
        skillSource: "test",
        status: "planned",
        assignment: assignmentFor("run-review", "backend", ["src"]),
      },
      {
        name: "reviewer",
        runner: "codex",
        readOnly: true,
        owns: [],
        dependsOn: ["backend"],
        promptFile: path.join(runDir, "reviewer.md"),
        prompt: "reviewer",
        command: process.execPath,
        args: [],
        skillSource: "test",
        status: "planned",
        assignment: assignmentFor("run-review", "reviewer", []),
      },
    ];
    const run: Run = {
      id: "run-review",
      featureId: "001",
      status: "planned",
      createdAt: new Date().toISOString(),
      roles,
    };
    const provider = commandCommunicationProvider({
      backend: (input) => {
        assignmentIds.push(input.assignment.assignmentId);
        const correction =
          input.assignment.assignmentId.includes("review-feedback");
        return {
          command: process.execPath,
          args: [
            "-e",
            `const fs=require("fs");fs.mkdirSync("src",{recursive:true});fs.writeFileSync("src/app.ts",${JSON.stringify(correction ? "export const value = 'good';\n" : "export const value = 'bad';\n")});console.log(${JSON.stringify(correction ? backendCorrection : backendInitial)});`,
          ],
        };
      },
      reviewer: (input) => {
        assignmentIds.push(input.assignment.assignmentId);
        const reReview =
          input.assignment.assignmentId.includes("review-feedback");
        return {
          command: process.execPath,
          args: [
            "-e",
            `const fs=require("fs");if(${reReview}&&!fs.readFileSync("src/app.ts","utf8").includes("good"))process.exit(2);console.log(${JSON.stringify(reReview ? approved : needsChanges)});`,
          ],
        };
      },
    });
    const eventRepository = new InMemoryRunEventRepository();
    const results = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      eventRepository,
      communicationProviders: [provider],
    });

    assert.equal(run.status, "completed");
    assert.equal(
      results.every((result) => result.status === "completed"),
      true,
    );
    assert.equal(new Set(assignmentIds).size, 4);
    assert.ok(assignmentIds.includes("run-review:backend:review-feedback:1"));
    assert.ok(assignmentIds.includes("run-review:reviewer:review-feedback:1"));
    assert.match(
      await readFile(path.join(run.roles[1]!.worktree!, "src/app.ts"), "utf8"),
      /good/,
    );
    assert.match(
      await readFile(path.join(projectRoot, "src/app.ts"), "utf8"),
      /good/,
    );
    const events = await eventRepository.loadByRun(run.id);
    assert.ok(
      events.some(
        (event) =>
          event.roleId === "system" &&
          event.payload.kind === "activity" &&
          event.payload.message.includes("integrated into the project"),
      ),
    );
    assert.ok(
      events.some(
        (event) =>
          event.roleId === "system" &&
          event.payload.kind === "activity" &&
          event.payload.message.includes("no required fixes"),
      ),
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(path.join(tmpdir(), ".conduit-worktrees"), {
      recursive: true,
      force: true,
    });
  }
});

test("approved reviewer promotion fails closed when the project changes during the run", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, readFile, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-promotion-dirty-"),
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
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-dirty");
    await mkdir(runDir, { recursive: true });
    const implementation =
      '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":null,"artifacts":[{"path":"src/app.ts","category":"source","purpose":"implementation","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const approved =
      '{"protocolVersion":"1.0","status":"completed","summary":"approved","verdict":{"decision":"approved","rationale":"verified"},"artifacts":[],"findings":[],"verification":[],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const role = (
      name: string,
      readOnly: boolean,
      dependsOn: string[],
      owns: string[],
    ): Run["roles"][number] => ({
      name,
      runner: "codex",
      readOnly,
      owns,
      dependsOn,
      promptFile: path.join(runDir, `${name}.md`),
      prompt: name,
      command: process.execPath,
      args: [],
      skillSource: "test",
      status: "planned",
      assignment: assignmentFor("run-dirty", name, owns),
    });
    const run: Run = {
      id: "run-dirty",
      featureId: "001",
      status: "planned",
      createdAt: new Date().toISOString(),
      roles: [
        role("backend", false, [], ["src"]),
        role("reviewer", true, ["backend"], []),
      ],
    };
    const events = new InMemoryRunEventRepository();
    const provider = commandCommunicationProvider({
      backend: {
        command: process.execPath,
        args: [
          "-e",
          `const fs=require("fs");fs.mkdirSync("src",{recursive:true});fs.writeFileSync("src/app.ts","ok");console.log(${JSON.stringify(implementation)});`,
        ],
      },
      reviewer: {
        command: process.execPath,
        args: [
          "-e",
          `require("fs").writeFileSync(${JSON.stringify(path.join(projectRoot, "LOCAL_CHANGE.txt"))},"local");console.log(${JSON.stringify(approved)});`,
        ],
      },
    });
    await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      eventRepository: events,
      communicationProviders: [provider],
    });

    assert.equal(run.status, "failed");
    await assert.rejects(readFile(path.join(projectRoot, "src/app.ts")));
    assert.equal(
      await readFile(path.join(run.roles[1]!.worktree!, "src/app.ts"), "utf8"),
      "ok",
    );
    assert.ok(
      (await events.loadByRun(run.id)).some(
        (event) =>
          event.payload.kind === "error" &&
          event.payload.code === "APPROVED_WORKTREE_INTEGRATION_FAILED",
      ),
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(path.join(tmpdir(), ".conduit-worktrees"), {
      recursive: true,
      force: true,
    });
  }
});

test("reviewer rejection cannot route a broadly owned forbidden path", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-review-route-"),
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
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-unowned");
    await mkdir(runDir, { recursive: true });
    const backend =
      '{"protocolVersion":"1.0","status":"completed","summary":"ok","verdict":null,"artifacts":[{"path":"src/app.ts","category":"source","purpose":"implementation","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const rejected =
      '{"protocolVersion":"1.0","status":"completed","summary":"changes","verdict":{"decision":"rejected","rationale":"Unowned change required."},"artifacts":[],"findings":[{"severity":"error","category":"correctness","message":"Fix config.","path":"config/app.yml","evidence":["config/app.yml"]}],"verification":[],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const role = (
      name: string,
      readOnly: boolean,
      dependsOn: string[],
      owns: string[],
    ): Run["roles"][number] => {
      const assignment = assignmentFor("run-unowned", name, owns);
      return {
        name,
        runner: "codex",
        readOnly,
        owns,
        dependsOn,
        promptFile: path.join(runDir, `${name}.md`),
        prompt: name,
        command: process.execPath,
        args: [],
        skillSource: "test",
        status: "planned",
        assignment:
          name === "backend"
            ? { ...assignment, forbiddenPaths: ["config"] }
            : assignment,
      };
    };
    const run: Run = {
      id: "run-unowned",
      featureId: "001",
      status: "planned",
      createdAt: new Date().toISOString(),
      roles: [
        role("backend", false, [], ["."]),
        role("reviewer", true, ["backend"], []),
      ],
    };
    const events = new InMemoryRunEventRepository();
    const provider = commandCommunicationProvider({
      backend: {
        command: process.execPath,
        args: [
          "-e",
          `const fs=require("fs");fs.mkdirSync("src",{recursive:true});fs.writeFileSync("src/app.ts","ok");console.log(${JSON.stringify(backend)});`,
        ],
      },
      reviewer: {
        command: process.execPath,
        args: ["-e", `console.log(${JSON.stringify(rejected)});`],
      },
    });
    const results = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      eventRepository: events,
      communicationProviders: [provider],
    });
    assert.equal(run.status, "failed");
    assert.equal(
      results.find((result) => result.role === "reviewer")?.status,
      "failed",
    );
    assert.ok(
      (await events.loadByRun(run.id)).some(
        (event) =>
          event.payload.kind === "error" &&
          event.payload.code === "REVIEW_FINDING_UNROUTABLE",
      ),
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(path.join(tmpdir(), ".conduit-worktrees"), {
      recursive: true,
      force: true,
    });
  }
});

test("review correction stops on repeated findings, no changes, generated reviewer artifacts, and the round limit", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, readFile, writeFile } = await import("node:fs/promises");
  for (const scenario of [
    "repeated",
    "no-change",
    "artifact",
    "limit",
  ] as const) {
    const projectRoot = await mkdtemp(
      path.join(tmpdir(), `conduit-review-${scenario}-`),
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
      await writeFile(
        path.join(projectRoot, ".gitignore"),
        "arbitrary-review-output/\n",
      );
      execFileSync("git", [
        "-C",
        projectRoot,
        "add",
        "README.md",
        ".gitignore",
      ]);
      execFileSync("git", [
        "-C",
        projectRoot,
        "-c",
        "commit.gpgSign=false",
        "commit",
        "-m",
        "init",
      ]);
      const runId = `run-${scenario}`;
      const runDir = path.join(projectRoot, ".conduit", "runs", runId);
      await mkdir(runDir, { recursive: true });
      const base = {
        protocolVersion: "1.0",
        status: "completed",
        summary: "ok",
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
      };
      const implementation = (action: "created" | "modified" | "inspected") =>
        JSON.stringify({
          ...base,
          verdict: null,
          artifacts: [
            {
              path: "src/app.ts",
              category: "source",
              purpose: "implementation",
              action,
            },
          ],
          verification: [
            { operation: "test", outcome: "passed", summary: "ok" },
          ],
        });
      const review = (message: string) =>
        JSON.stringify({
          ...base,
          verdict: {
            decision: "needs_changes",
            rationale: "Correction required.",
          },
          findings: [
            {
              severity: "error",
              category: "correctness",
              message,
              path: "src/app.ts",
              evidence: ["src/app.ts"],
            },
          ],
        });
      const role = (
        name: string,
        readOnly: boolean,
        dependsOn: string[],
        owns: string[],
      ): Run["roles"][number] => ({
        name,
        runner: "codex",
        readOnly,
        owns,
        dependsOn,
        promptFile: path.join(runDir, `${name}.md`),
        prompt: name,
        command: process.execPath,
        args: [],
        skillSource: "test",
        status: "planned",
        assignment: assignmentFor(runId, name, owns),
      });
      const run: Run = {
        id: runId,
        featureId: "001",
        status: "planned",
        createdAt: new Date().toISOString(),
        roles: [
          role("backend", false, [], ["src"]),
          role("reviewer", true, ["backend"], []),
        ],
      };
      const seenAssignments: string[] = [];
      const provider = commandCommunicationProvider({
        backend: (input) => {
          seenAssignments.push(input.assignment.assignmentId);
          const match = input.assignment.assignmentId.match(
            /review-feedback:(\d+)$/,
          );
          const round = Number(match?.[1] ?? 0);
          const writes = round === 0 || scenario !== "no-change";
          const script = writes
            ? `const fs=require("fs");fs.mkdirSync("src",{recursive:true});fs.writeFileSync("src/app.ts","round-${round}");`
            : "";
          return {
            command: process.execPath,
            args: [
              "-e",
              `${script}console.log(${JSON.stringify(implementation(round === 0 ? "created" : writes ? "modified" : "inspected"))});`,
            ],
          };
        },
        reviewer: (input) => {
          seenAssignments.push(input.assignment.assignmentId);
          const match = input.assignment.assignmentId.match(
            /review-feedback:(\d+)$/,
          );
          const round = Number(match?.[1] ?? 0);
          const message =
            scenario === "limit"
              ? `Still broken round ${round}.`
              : "Still broken.";
          const generatedArtifact =
            scenario === "artifact" && round === 0
              ? 'const fs=require("fs");fs.mkdirSync("arbitrary-review-output",{recursive:true});fs.writeFileSync("arbitrary-review-output/reviewer.tmp","generated");'
              : "";
          return {
            command: process.execPath,
            args: [
              "-e",
              `${generatedArtifact}console.log(${JSON.stringify(review(message))});`,
            ],
          };
        },
      });
      const events = new InMemoryRunEventRepository();
      await executeRun({
        projectRoot,
        run,
        runDir,
        dryRun: false,
        eventRepository: events,
        communicationProviders: [provider],
      });
      assert.equal(run.status, "failed", scenario);
      const correctionAssignments = seenAssignments.filter((id) =>
        id.includes("backend:review-feedback"),
      );
      assert.equal(
        correctionAssignments.length,
        scenario === "limit" ? 2 : 1,
        scenario,
      );
      const runEvents = await events.loadByRun(runId);
      assert.equal(
        runEvents.some(
          (event) =>
            event.roleId === "system" &&
            event.payload.kind === "activity" &&
            event.payload.message.includes("no required fixes"),
        ),
        false,
      );
      const expectedErrorCode = {
        repeated: "REPEATED_REVIEW_FINDINGS",
        "no-change": "REVIEW_CORRECTION_NO_CHANGES",
        artifact: "REPEATED_REVIEW_FINDINGS",
        limit: "REVIEW_CORRECTION_LIMIT_EXHAUSTED",
      }[scenario];
      assert.ok(
        runEvents.some(
          (event) =>
            event.payload.kind === "error" &&
            event.payload.code === expectedErrorCode,
        ),
        scenario,
      );
      if (scenario === "artifact")
        await assert.rejects(
          readFile(
            path.join(
              run.roles[1]!.worktree!,
              "arbitrary-review-output/reviewer.tmp",
            ),
          ),
        );
    } finally {
      await rm(projectRoot, { recursive: true, force: true });
      await rm(path.join(tmpdir(), ".conduit-worktrees"), {
        recursive: true,
        force: true,
      });
    }
  }
});

test("resume reuses failed role worktrees and preserves completed roles", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, readFile, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-resume-"));
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
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-resume");
    await mkdir(runDir, { recursive: true });
    const frontendResponse =
      '{"protocolVersion":"1.0","status":"completed","summary":"implemented","verdict":null,"artifacts":[{"path":"src/app.ts","category":"source","purpose":"implementation","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const qaResponse =
      '{"protocolVersion":"1.0","status":"completed","summary":"verified","verdict":null,"artifacts":[{"path":"tests/app.test.ts","category":"test","purpose":"verification","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const role = (
      name: string,
      owns: string[],
      dependsOn: string[] = [],
    ): Run["roles"][number] => ({
      name,
      runner: "codex",
      readOnly: false,
      owns,
      dependsOn,
      promptFile: path.join(runDir, `${name}.md`),
      prompt: name,
      command: process.execPath,
      args: [],
      skillSource: "test",
      status: "planned",
      assignment: assignmentFor("run-resume", name, owns),
    });
    const run: Run = {
      id: "run-resume",
      featureId: "001",
      status: "planned",
      createdAt: new Date().toISOString(),
      roles: [role("frontend", ["src"]), role("qa", ["tests"], ["frontend"])],
    };
    let frontendCalls = 0;
    let qaCalls = 0;
    const provider = commandCommunicationProvider({
      frontend: () => {
        frontendCalls += 1;
        return {
          command: process.execPath,
          args: [
            "-e",
            `const fs=require("fs");fs.mkdirSync("src",{recursive:true});fs.writeFileSync("src/app.ts","ok");console.log(${JSON.stringify(frontendResponse)});`,
          ],
        };
      },
      qa: (input) => {
        qaCalls += 1;
        const resumed = input.assignment.assignmentId.includes(":resume:");
        return {
          command: process.execPath,
          args: [
            "-e",
            resumed
              ? `const fs=require("fs");if(!fs.existsSync("src/app.ts"))process.exit(2);fs.mkdirSync("tests",{recursive:true});fs.writeFileSync("tests/app.test.ts","ok");console.log(${JSON.stringify(qaResponse)});`
              : 'const fs=require("fs");fs.mkdirSync("tests",{recursive:true});fs.writeFileSync("tests/app.test.ts","draft");console.log("not-json");',
          ],
        };
      },
    });
    const first = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      maxAutomaticRetries: 0,
      communicationProviders: [provider],
    });
    assert.equal(run.status, "failed");
    assert.equal(
      first.find((result) => result.role === "frontend")?.status,
      "completed",
    );
    const qaWorktree = run.roles[1]!.worktree;

    const resumed = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      resume: true,
      maxAutomaticRetries: 0,
      communicationProviders: [provider],
    });

    assert.equal(run.status, "completed");
    assert.equal(frontendCalls, 1);
    assert.equal(qaCalls, 2);
    assert.equal(run.roles[1]!.worktree, qaWorktree);
    assert.match(run.roles[1]!.assignment!.assignmentId, /:resume:1$/);
    assert.equal(
      resumed.every((result) => result.status === "completed"),
      true,
    );
    assert.equal(
      await readFile(path.join(qaWorktree!, "tests/app.test.ts"), "utf8"),
      "ok",
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(path.join(tmpdir(), ".conduit-worktrees"), {
      recursive: true,
      force: true,
    });
  }
});

test("reviewer-only resume after restart preserves completed implementation work", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-review-resume-"),
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
    const startingHead = execFileSync(
      "git",
      ["-C", projectRoot, "rev-parse", "HEAD"],
      { encoding: "utf8" },
    ).trim();
    const runDir = path.join(
      projectRoot,
      ".conduit",
      "runs",
      "run-review-resume",
    );
    await mkdir(runDir, { recursive: true });
    const response = (value: object) =>
      JSON.stringify({
        protocolVersion: "1.0",
        status: "completed",
        summary: "ok",
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
        ...value,
      });
    const implementation = response({
      artifacts: [
        {
          path: "src/app.ts",
          category: "source",
          purpose: "implementation",
          action: "created",
        },
      ],
      verification: [{ operation: "test", outcome: "passed", summary: "ok" }],
    });
    const approved = response({
      verdict: { decision: "approved", rationale: "Verified after restart." },
    });
    const role = (
      name: string,
      readOnly: boolean,
      owns: string[],
      dependsOn: string[] = [],
    ): Run["roles"][number] => ({
      name,
      runner: "codex",
      readOnly,
      owns,
      dependsOn,
      promptFile: path.join(runDir, `${name}.md`),
      prompt: name,
      command: process.execPath,
      args: [],
      skillSource: "test",
      status: "planned",
      assignment: assignmentFor("run-review-resume", name, owns),
    });
    const run: Run = {
      id: "run-review-resume",
      featureId: "008",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead,
      roles: [
        role("backend", false, ["src"]),
        role("reviewer", true, [], ["backend"]),
      ],
    };
    let backendCalls = 0;
    let reviewerCalls = 0;
    const firstProvider = commandCommunicationProvider({
      backend: () => {
        backendCalls += 1;
        return {
          command: process.execPath,
          args: [
            "-e",
            `const fs=require("fs");fs.mkdirSync("src",{recursive:true});fs.writeFileSync("src/app.ts","ok");console.log(${JSON.stringify(implementation)});`,
          ],
        };
      },
      reviewer: () => {
        reviewerCalls += 1;
        return {
          command: process.execPath,
          args: ["-e", 'console.log("not-json")'],
        };
      },
    });
    await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      maxAutomaticRetries: 0,
      communicationProviders: [firstProvider],
    });
    assert.equal(run.status, "failed");
    assert.equal(run.roles[0]!.status, "completed");

    const restored = JSON.parse(JSON.stringify(run)) as Run;
    const resumedProvider = commandCommunicationProvider({
      backend: () => {
        backendCalls += 1;
        return { command: process.execPath, args: ["-e", "process.exit(9)"] };
      },
      reviewer: () => {
        reviewerCalls += 1;
        return {
          command: process.execPath,
          args: ["-e", `console.log(${JSON.stringify(approved)});`],
        };
      },
    });
    await executeRun({
      projectRoot,
      run: restored,
      runDir,
      dryRun: false,
      resume: true,
      maxAutomaticRetries: 0,
      communicationProviders: [resumedProvider],
    });
    assert.equal(restored.status, "completed");
    assert.equal(backendCalls, 1);
    assert.equal(reviewerCalls, 2);
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(path.join(tmpdir(), ".conduit-worktrees"), {
      recursive: true,
      force: true,
    });
  }
});

test("failed agent turns automatically retry with Conduit validation feedback", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-auto-retry-"));
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
    await mkdir(path.join(projectRoot, "node_modules", "fixture"), {
      recursive: true,
    });
    await writeFile(
      path.join(projectRoot, "node_modules", "fixture", "index.js"),
      "fixture\n",
    );
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
    const runDir = path.join(projectRoot, ".conduit", "runs", "run-auto");
    await mkdir(runDir, { recursive: true });
    const invalid =
      '{"protocolVersion":"1.0","status":"completed","summary":"documented","verdict":null,"artifacts":[{"path":"docs/guide.md","category":"documentation","purpose":"guide","action":"created"}],"findings":[],"verification":[{"operation":"docs check","outcome":"failed","summary":"cleanup was denied"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const valid = invalid.replace('"outcome":"failed"', '"outcome":"passed"');
    const role: Run["roles"][number] = {
      name: "documentation",
      runner: "codex",
      readOnly: false,
      owns: ["docs"],
      dependsOn: [],
      promptFile: path.join(runDir, "documentation.md"),
      prompt: "documentation",
      command: process.execPath,
      args: [],
      skillSource: "test",
      status: "planned",
      assignment: assignmentFor("run-auto", "documentation", ["docs"]),
    };
    role.assignment = {
      ...role.assignment!,
      objective: `Resume failed attempt 9. Original assignment objective: ${role.assignment!.objective}`,
    };
    const run: Run = {
      id: "run-auto",
      featureId: "001",
      status: "planned",
      createdAt: new Date().toISOString(),
      roles: [role],
    };
    let calls = 0;
    let retryObjective = "";
    const retryWorkspaces = new Set<string>();
    const provider = commandCommunicationProvider({
      documentation: (input) => {
        calls += 1;
        const retry = input.assignment.assignmentId.includes(":resume:");
        if (retry) {
          retryObjective = input.assignment.objective;
          retryWorkspaces.add(input.workspaceRoot);
        }
        return {
          command: process.execPath,
          args: [
            "-e",
            `const fs=require("fs");fs.mkdirSync("docs",{recursive:true});fs.writeFileSync("docs/guide.md","guide");fs.mkdirSync("dist/assets",{recursive:true});fs.writeFileSync("dist/assets/generated.js","generated");console.log(${JSON.stringify(calls >= 3 ? valid : invalid)});`,
          ],
        };
      },
    });
    const events = new InMemoryRunEventRepository();

    await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      eventRepository: events,
      maxAutomaticRetries: 99,
      communicationProviders: [provider],
    });

    assert.equal(run.status, "completed");
    assert.equal(calls, 3);
    assert.equal(retryWorkspaces.size, 1);
    assert.deepEqual(role.pendingResumeCommits, []);
    assert.ok((role.integrationCommits?.length ?? 0) >= 1);
    assert.match(retryObjective, /Conduit rejected the previous turn/);
    assert.match(retryObjective, /verification outcome to be passed/);
    assert.equal(
      retryObjective.match(/Original assignment objective:/g)?.length,
      1,
    );
    assert.ok(retryObjective.length <= 2_400);
    assert.ok(
      (await events.loadByRun(run.id)).some(
        (event) =>
          event.payload.kind === "activity" &&
          event.payload.message.includes("automatically retrying"),
      ),
    );
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(path.join(tmpdir(), ".conduit-worktrees"), {
      recursive: true,
      force: true,
    });
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
            runner: "codex",
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
            runner: "codex",
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
        communicationProviders: [
          commandCommunicationProvider(
            Object.fromEntries(
              run.roles.map((role) => [
                role.name,
                { command: role.command, args: role.args },
              ]),
            ),
          ),
        ],
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

test("legacy retention never removes a registered worktree without canonical identity", async () => {
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
    await repository.save({
      runId: "failed-retained",
      status: "failed",
      worktrees: [projectRoot],
      completedAt: "2020-01-01T00:00:00.000Z",
    });

    await cleanupExpiredWorktrees(projectRoot, stateDirectory, 0);

    assert.equal(
      await readFile(path.join(worktree, "README.md"), "utf8"),
      "base\n",
    );
    assert.deepEqual(
      (await repository.listExpired(new Date())).map((record) => record.runId),
      ["expired", "failed-retained"],
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

test("managed role workspace conflicts fail before provider launch", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { TursoRoleWorkspaceRepository } =
    await import("../src/domains/runs/repositories/turso-role-workspace-repository.js");
  const { ProjectDatabaseFactory } =
    await import("../src/system/storage/factories/database-factories.js");
  const { resolveRepositoryIdentity } =
    await import("../src/domains/runs/services/role-workspace-identity-service.js");
  const { RoleWorkspaceState } =
    await import("../src/domains/runs/enums/role-workspace-state.js");
  const { execFileSync } = await import("node:child_process");
  const { writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-role-conflict-"),
  );
  const connection = await new ProjectDatabaseFactory(projectRoot).open();
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
    execFileSync("git", ["-C", projectRoot, "add", "."]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "-m",
      "base",
    ]);
    const head = execFileSync("git", ["-C", projectRoot, "rev-parse", "HEAD"], {
      encoding: "utf8",
    }).trim();
    const slots = new TursoRoleWorkspaceRepository(connection);
    const repositoryId = resolveRepositoryIdentity(projectRoot).repositoryId;
    const pending = await slots.claim({
      repositoryId,
      roleKey: "old",
      workspacePath: path.join(
        projectRoot,
        ".conduit",
        "worktrees",
        repositoryId,
        "old",
      ),
      owningRunId: "old-cleanup",
      startingHead: head,
      packageHash: "b".repeat(64),
      assignmentHash: "c".repeat(64),
      branchName: "conduit/old/role",
      leaseOwner: "old-cleanup:old",
    });
    assert.equal(pending.status, "claimed");
    if (pending.status !== "claimed") return;
    const pendingIdentity = {
      repositoryId,
      roleKey: "old",
      owningRunId: "old-cleanup",
      leaseOwner: "old-cleanup:old",
      fencingToken: pending.slot.fencingToken,
    };
    assert.equal(
      await slots.transition(
        pendingIdentity,
        RoleWorkspaceState.Provisioning,
        RoleWorkspaceState.CleanupPending,
      ),
      true,
    );
    assert.equal(
      await slots.completeGeneration(pendingIdentity, {
        branchOid: head,
        outcome: "promoted",
        promotionOid: head,
      }),
      true,
    );
    await slots.claim({
      repositoryId,
      roleKey: "worker",
      workspacePath: path.join(projectRoot, ".slots", "worker"),
      owningRunId: "other-run",
      startingHead: head,
      packageHash: "b".repeat(64),
      assignmentHash: "c".repeat(64),
      branchName: "conduit/other/worker",
      leaseOwner: "other:worker",
    });
    const assignment = assignmentFor("new-run", "worker", ["src"]);
    const run: Run = {
      id: "new-run",
      featureId: "009",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "b".repeat(64),
      roles: [
        {
          name: "worker",
          runner: "codex",
          readOnly: false,
          owns: ["src"],
          dependsOn: [],
          promptFile: "",
          prompt: "",
          command: "",
          args: [],
          skillSource: "test",
          status: "planned",
          assignment,
          worktree: path.join(projectRoot, ".slots", "worker"),
          workspaceRepositoryId: repositoryId,
          workspaceRoleKey: "worker",
          workspaceBranchName: "conduit/new/worker",
          workspaceAssignmentHash: createHash("sha256")
            .update(JSON.stringify(assignment))
            .digest("hex"),
          workspaceLeaseOwner: "new-run:worker",
        },
      ],
    };
    let providerCalls = 0;
    const provider = commandCommunicationProvider({
      worker: () => {
        providerCalls += 1;
        return { command: process.execPath, args: ["-e", ""] };
      },
    });
    await assert.rejects(
      () =>
        executeRun({
          projectRoot,
          run,
          runDir: path.join(projectRoot, ".conduit", "runs", "new-run"),
          dryRun: false,
          communicationProviders: [provider],
          roleWorkspaceRepository: slots,
        }),
      /retained by run other-run/,
    );
    assert.equal(providerCalls, 0);
    assert.equal(await slots.load(repositoryId, "old"), undefined);
  } finally {
    await connection.close();
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("approved managed runs remove exact role worktrees and preserve lineage", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { TursoRoleWorkspaceRepository } =
    await import("../src/domains/runs/repositories/turso-role-workspace-repository.js");
  const { ProjectDatabaseFactory } =
    await import("../src/system/storage/factories/database-factories.js");
  const { resolveRepositoryIdentity } =
    await import("../src/domains/runs/services/role-workspace-identity-service.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-role-cleanup-"),
  );
  const connection = await new ProjectDatabaseFactory(projectRoot).open();
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
    execFileSync("git", ["-C", projectRoot, "add", "."]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "-m",
      "base",
    ]);
    const head = execFileSync("git", ["-C", projectRoot, "rev-parse", "HEAD"], {
      encoding: "utf8",
    }).trim();
    const repositoryId = resolveRepositoryIdentity(projectRoot).repositoryId;
    const runDir = path.join(
      projectRoot,
      ".conduit",
      "runs",
      "managed-cleanup",
    );
    await mkdir(runDir, { recursive: true });
    const workerAssignment = assignmentFor("managed-cleanup", "worker", [
      "src",
    ]);
    const reviewerAssignment = assignmentFor("managed-cleanup", "reviewer", []);
    const role = (
      name: string,
      assignment: ReturnType<typeof assignmentFor>,
      readOnly: boolean,
      dependsOn: string[],
    ): Run["roles"][number] => ({
      name,
      runner: "codex",
      readOnly,
      owns: readOnly ? [] : ["src"],
      dependsOn,
      promptFile: "",
      prompt: "",
      command: "",
      args: [],
      skillSource: "test",
      status: "planned",
      assignment,
      worktree: path.join(
        projectRoot,
        ".conduit",
        "worktrees",
        repositoryId,
        name,
      ),
      workspaceRepositoryId: repositoryId,
      workspaceRoleKey: name,
      workspaceBranchName: `conduit/managed-cleanup/${name}`,
      workspaceAssignmentHash: createHash("sha256")
        .update(JSON.stringify(assignment))
        .digest("hex"),
      workspaceLeaseOwner: `managed-cleanup:${name}`,
    });
    const run: Run = {
      id: "managed-cleanup",
      featureId: "009",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "b".repeat(64),
      roles: [
        role("worker", workerAssignment, false, []),
        role("reviewer", reviewerAssignment, true, ["worker"]),
      ],
      reviewerWorkflow: { correctionRound: 0, findingFingerprints: [] },
    };
    const workerResponse =
      '{"protocolVersion":"1.0","status":"completed","summary":"done","verdict":null,"artifacts":[{"path":"src/work.txt","category":"source","purpose":"work","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const reviewerResponse =
      '{"protocolVersion":"1.0","status":"completed","summary":"approved","verdict":{"decision":"approved","rationale":"verified"},"artifacts":[],"findings":[],"verification":[{"operation":"review","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const provider = commandCommunicationProvider({
      worker: {
        command: process.execPath,
        args: [
          "-e",
          `const fs=require('fs');fs.mkdirSync('src',{recursive:true});fs.writeFileSync('src/work.txt','ok');console.log(${JSON.stringify(workerResponse)})`,
        ],
      },
      reviewer: {
        command: process.execPath,
        args: ["-e", `console.log(${JSON.stringify(reviewerResponse)})`],
      },
    });
    const slots = new TursoRoleWorkspaceRepository(connection);
    const paths = run.roles.map((item) => item.worktree!);
    const results = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      communicationProviders: [provider],
      roleWorkspaceRepository: slots,
    });
    assert.equal(run.status, "completed", JSON.stringify(results));
    const remainingSlots = await slots.listByRun(run.id);
    assert.equal(remainingSlots.length, 0, JSON.stringify(remainingSlots));
    for (const workspace of paths)
      assert.equal(
        await import("node:fs/promises").then(({ stat }) =>
          stat(workspace)
            .then(() => true)
            .catch(() => false),
        ),
        false,
      );
    for (const name of ["worker", "reviewer"])
      assert.throws(() =>
        execFileSync("git", [
          "-C",
          projectRoot,
          "rev-parse",
          `refs/heads/conduit/managed-cleanup/${name}`,
        ]),
      );
    const generations = await Promise.all(
      ["worker", "reviewer"].map((name) =>
        slots.listGenerations(repositoryId, name),
      ),
    );
    assert.equal(
      generations.every((items) => items[0]?.outcome === "promoted"),
      true,
    );
  } finally {
    await connection.close();
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("role workspace slots retain failed work and resume in the same path", async () => {
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { TursoRoleWorkspaceRepository } =
    await import("../src/domains/runs/repositories/turso-role-workspace-repository.js");
  const { ProjectDatabaseFactory } =
    await import("../src/system/storage/factories/database-factories.js");
  const { RoleWorkspaceState } =
    await import("../src/domains/runs/enums/role-workspace-state.js");
  const { resolveRepositoryIdentity } =
    await import("../src/domains/runs/services/role-workspace-identity-service.js");
  const { execFileSync } = await import("node:child_process");
  const { mkdir, writeFile } = await import("node:fs/promises");
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-role-resume-"),
  );
  let connection:
    | Awaited<ReturnType<InstanceType<typeof ProjectDatabaseFactory>["open"]>>
    | undefined;
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
    execFileSync("git", ["-C", projectRoot, "add", "."]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "-m",
      "base",
    ]);
    const head = execFileSync("git", ["-C", projectRoot, "rev-parse", "HEAD"], {
      encoding: "utf8",
    }).trim();
    const repositoryId = resolveRepositoryIdentity(projectRoot).repositoryId;
    const runDir = path.join(projectRoot, ".conduit", "runs", "role-resume");
    await mkdir(runDir, { recursive: true });
    const assignment = assignmentFor("role-resume", "worker", ["src"]);
    const worktree = path.join(projectRoot, ".slots", "repo", "worker");
    const role: Run["roles"][number] = {
      name: "worker",
      runner: "codex",
      readOnly: false,
      owns: ["src"],
      dependsOn: [],
      promptFile: "",
      prompt: "",
      command: "",
      args: [],
      skillSource: "test",
      status: "planned",
      assignment,
      worktree,
      workspaceRepositoryId: repositoryId,
      workspaceRoleKey: "worker",
      workspaceBranchName: "conduit/role-resume/worker",
      workspaceAssignmentHash: createHash("sha256")
        .update(JSON.stringify(assignment))
        .digest("hex"),
      workspaceLeaseOwner: "role-resume:worker",
    };
    const run: Run = {
      id: "role-resume",
      featureId: "009",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "b".repeat(64),
      roles: [role],
    };
    connection = await new ProjectDatabaseFactory(projectRoot).open();
    const slots = new TursoRoleWorkspaceRepository(connection);
    let calls = 0;
    let persistedBeforeProvider = false;
    const persistWorkspace = async () => {
      if (role.worktreeHead) persistedBeforeProvider = true;
    };
    const valid =
      '{"protocolVersion":"1.0","status":"completed","summary":"done","verdict":null,"artifacts":[{"path":"src/work.txt","category":"source","purpose":"work","action":"created"}],"findings":[],"verification":[{"operation":"test","outcome":"passed","summary":"ok"}],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[],"memoryProposals":[],"globalPromotionProposals":[]}';
    const provider = commandCommunicationProvider({
      worker: () => {
        assert.equal(persistedBeforeProvider, true);
        return {
          command: process.execPath,
          args: [
            "-e",
            `const fs=require('fs');fs.mkdirSync('src',{recursive:true});fs.writeFileSync('src/work.txt','ok');console.log(${JSON.stringify(++calls === 1 ? "not-json" : valid)})`,
          ],
        };
      },
    });
    await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      maxAutomaticRetries: 0,
      communicationProviders: [provider],
      roleWorkspaceRepository: slots,
      onRoleWorkspaceReady: persistWorkspace,
    });
    assert.equal(run.status, "failed");
    assert.equal(
      (await slots.load(repositoryId, "worker"))?.state,
      RoleWorkspaceState.Retained,
    );
    const retainedPath = role.worktree;
    await mkdir(path.join(retainedPath!, "dist", "assets"), {
      recursive: true,
    });
    await writeFile(
      path.join(retainedPath!, "dist", "assets", "generated.js"),
      "generated\n",
    );
    persistedBeforeProvider = false;
    const resumed = await executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      resume: true,
      maxAutomaticRetries: 0,
      communicationProviders: [provider],
      roleWorkspaceRepository: slots,
      onRoleWorkspaceReady: persistWorkspace,
    });
    assert.equal(run.status, "completed", JSON.stringify(resumed));
    assert.equal(role.worktree, retainedPath);
    assert.equal(
      await import("node:fs/promises").then(({ readFile }) =>
        readFile(
          path.join(retainedPath!, "dist", "assets", "generated.js"),
          "utf8",
        ),
      ),
      "generated\n",
    );
    assert.ok((role.integrationCommits?.length ?? 0) >= 1);
    assert.equal(
      (await slots.load(repositoryId, "worker"))?.state,
      RoleWorkspaceState.Retained,
    );
  } finally {
    await connection?.close();
    await rm(projectRoot, { recursive: true, force: true });
  }
});
