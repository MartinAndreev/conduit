import { describe, expect, test } from "bun:test";
import { roleExecutionStages } from "../src/domains/runs/repositories/run-orchestrator.js";
import type { RunRole } from "../src/domains/runs/types/run.js";

function role(name: string, dependsOn: string[] = []): RunRole {
  return {
    name,
    runner: "opencode",
    readOnly: false,
    owns: [],
    dependsOn,
    promptFile: "",
    prompt: "",
    command: "true",
    args: [],
    skillSource: "",
    status: "planned",
  };
}

describe("role execution stages", () => {
  test("follow configured dependencies", () => {
    const stages = roleExecutionStages([
      role("frontend"),
      role("backend"),
      role("qa", ["frontend", "backend"]),
      role("documentation", ["frontend", "backend"]),
      role("reviewer", ["qa", "documentation"]),
    ]);
    expect(stages.map((stage) => stage.map((item) => item.name))).toEqual([
      ["frontend", "backend"],
      ["qa", "documentation"],
      ["reviewer"],
    ]);
  });

  test("ignore dependencies outside selected roles", () => {
    const stages = roleExecutionStages([
      role("frontend"),
      role("qa", ["frontend", "backend"]),
    ]);
    expect(stages.map((stage) => stage.map((item) => item.name))).toEqual([
      ["frontend"],
      ["qa"],
    ]);
  });

  test("reject dependency cycles", () => {
    expect(() =>
      roleExecutionStages([role("a", ["b"]), role("b", ["a"])]),
    ).toThrow(/dependency cycle/i);
  });
});

test("executeRun persists role worktrees before agent completion and emits flow completion", async () => {
  const { chmod, mkdtemp, mkdir, readFile, rm, writeFile } =
    await import("node:fs/promises");
  const os = await import("node:os");
  const path = await import("node:path");
  const { spawnSync } = await import("node:child_process");
  const { executeRun } =
    await import("../src/domains/runs/repositories/run-orchestrator.js");
  const { FileRunEventRepository } =
    await import("../src/domains/runs/repositories/file-run-event-repository.js");

  const projectRoot = await mkdtemp(path.join(os.tmpdir(), "conduit-run-"));
  const runDir = path.join(projectRoot, ".conduit", "runs", "run-1");
  const gateFile = path.join(projectRoot, "gate");
  const binDir = path.join(projectRoot, "bin");
  const fakeOpenCode = path.join(binDir, "opencode");
  await mkdir(runDir, { recursive: true });
  await mkdir(binDir, { recursive: true });
  let originalPath: string | undefined;
  try {
    spawnSync("git", ["init"], { cwd: projectRoot, encoding: "utf8" });
    spawnSync("git", ["config", "user.email", "test@example.com"], {
      cwd: projectRoot,
      encoding: "utf8",
    });
    spawnSync("git", ["config", "user.name", "Test User"], {
      cwd: projectRoot,
      encoding: "utf8",
    });
    await writeFile(path.join(projectRoot, "README.md"), "# demo\n");
    spawnSync("git", ["add", "README.md"], {
      cwd: projectRoot,
      encoding: "utf8",
    });
    spawnSync("git", ["commit", "-m", "init"], {
      cwd: projectRoot,
      encoding: "utf8",
    });
    await writeFile(
      fakeOpenCode,
      `#!/usr/bin/env bash
set -euo pipefail
while [ ! -f "$CONDUIT_TEST_GATE" ]; do sleep 0.01; done
printf 'done\n' > worker-output.txt
`,
    );
    await chmod(fakeOpenCode, 0o755);
    originalPath = process.env.PATH;
    process.env.PATH = `${binDir}:${originalPath ?? ""}`;
    process.env.CONDUIT_TEST_GATE = gateFile;
    const run = {
      id: "run-1",
      featureId: "001",
      status: "planned" as const,
      createdAt: new Date().toISOString(),
      roles: [role("frontend")],
    };
    run.roles[0].command = "opencode";
    run.roles[0].args = ["run", run.roles[0].promptFile];
    run.roles[0].promptFile = path.join(runDir, "frontend.md");
    run.roles[0].prompt = "# frontend";

    const eventRepository = new FileRunEventRepository(
      path.join(projectRoot, ".conduit"),
    );
    const execution = executeRun({
      projectRoot,
      run,
      runDir,
      dryRun: false,
      eventRepository,
    });

    let persistedWorktree: string | undefined;
    for (let index = 0; index < 100; index += 1) {
      const persisted = JSON.parse(
        await readFile(path.join(runDir, "run.json"), "utf8").catch(() => "{}"),
      ) as { roles?: Array<{ worktree?: string }> };
      persistedWorktree = persisted.roles?.[0]?.worktree;
      if (persistedWorktree) break;
      await new Promise((resolve) => setTimeout(resolve, 10));
    }

    expect(persistedWorktree).toBeTruthy();
    await writeFile(gateFile, "go\n");
    const results = await execution;
    expect(results[0]?.status).toBe("completed");

    const events = await eventRepository.loadByRun("run-1");
    expect(
      events.some(
        (event) =>
          event.roleId === "system" &&
          event.type === "lifecycle" &&
          event.payload.kind === "lifecycle" &&
          event.payload.state === "completed",
      ),
    ).toBe(true);
  } finally {
    if (originalPath === undefined) delete process.env.PATH;
    else process.env.PATH = originalPath;
    delete process.env.CONDUIT_TEST_GATE;
    await rm(projectRoot, { recursive: true, force: true });
  }
});
