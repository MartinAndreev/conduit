import { mkdir, readFile, writeFile, readdir } from "node:fs/promises";
import path from "node:path";
import { spawn, spawnSync } from "node:child_process";
import type { ChildProcess, SpawnSyncReturns } from "node:child_process";
import { resolveSkill } from "./skills.js";
import type { Config } from "./domains/configuration/types/config.js";
import type { Run, RunRole, RunResult } from "./domains/runs/types/run.js";

interface RunnerAdapter {
  command: string;
  beforeModel: string[];
  afterModel: string[];
}

const runnerAdapters: Record<string, RunnerAdapter> = {
  opencode: { command: "opencode", beforeModel: ["run"], afterModel: [] },
  codex: { command: "codex", beforeModel: ["exec"], afterModel: [] },
  pi: { command: "pi", beforeModel: [], afterModel: ["-p"] },
  kilo: { command: "kilo", beforeModel: ["run"], afterModel: [] },
};

export function commandForRole(
  role: { runner: string; model?: string },
  promptFile: string,
): [string, string[]] {
  const adapter = runnerAdapters[role.runner];
  if (!adapter)
    throw new Error(
      `Unsupported runner: ${role.runner}. Supported runners: ${Object.keys(runnerAdapters).join(", ")}.`,
    );
  const model = role.model ? ["--model", role.model] : [];
  const prompt = `Read ${promptFile} and perform only your assigned task.`;
  return [
    adapter.command,
    [...adapter.beforeModel, ...model, ...adapter.afterModel, prompt],
  ];
}

function pathsOverlap(left: string[] = [], right: string[] = []): boolean {
  return left.some((a) =>
    right.some(
      (b) => a === b || a.startsWith(`${b}/`) || b.startsWith(`${a}/`),
    ),
  );
}

function assertDistinctOwnership(roles: RunRole[]): void {
  for (let index = 0; index < roles.length; index += 1) {
    for (let other = index + 1; other < roles.length; other += 1) {
      if (roles[index].readOnly || roles[other].readOnly) continue;
      if (pathsOverlap(roles[index].owns, roles[other].owns)) {
        throw new Error(
          `Roles ${roles[index].name} and ${roles[other].name} have overlapping owned paths.`,
        );
      }
    }
  }
}

export async function planRun({
  projectRoot,
  config,
  featureId,
  roleNames,
  builtinRoot,
  fetchSkills = false,
}: {
  projectRoot: string;
  config: Config;
  featureId: string;
  roleNames: string[];
  builtinRoot: string;
  fetchSkills?: boolean;
}): Promise<{ run: Run; runDir: string }> {
  const runId = `${featureId}-${Date.now()}`;
  const runDir = path.join(projectRoot, config.stateDir, "runs", runId);
  await mkdir(runDir, { recursive: true });
  const roles: RunRole[] = [];
  for (const name of roleNames) {
    const role = config.roles[name];
    if (!role) throw new Error(`Unknown role: ${name}`);
    const skill = await resolveSkill({
      projectRoot,
      roleName: name,
      role,
      builtinRoot,
      allowNetwork: fetchSkills,
    });
    const promptFile = path.join(runDir, `${name}.md`);
    const prompt = `${skill.content}\n\n---\n\n# Assignment\n\nFeature: ${featureId}\nRead the approved files in specs/${featureId}-*/ before changing code.\nOwned paths: ${(role.owns ?? ["none defined"]).join(", ")}\nDo not modify contracts or paths outside your ownership.\nReport tests run and unresolved integration risks.\n`;
    await writeFile(promptFile, prompt);
    const [command, args] = commandForRole(role, promptFile);
    roles.push({
      name,
      runner: role.runner,
      readOnly: Boolean(role.readOnly),
      owns: role.owns ?? [],
      promptFile,
      prompt,
      command,
      args,
      skillSource: skill.source,
      status: "planned",
    });
  }
  assertDistinctOwnership(roles);
  const run: Run = {
    id: runId,
    featureId,
    status: "planned",
    createdAt: new Date().toISOString(),
    roles,
  };
  await writeFile(path.join(runDir, "run.json"), JSON.stringify(run, null, 2));
  return { run, runDir };
}

function worktreePath(projectRoot: string, run: Run, role: RunRole): string {
  return path.join(
    path.dirname(projectRoot),
    ".conduit-worktrees",
    path.basename(projectRoot),
    run.id,
    role.name,
  );
}

function addWorktree(projectRoot: string, run: Run, role: RunRole): string {
  const target = worktreePath(projectRoot, run, role);
  const branch = `conduit/${run.id}/${role.name}`;
  const result: SpawnSyncReturns<string> = spawnSync(
    "git",
    ["-C", projectRoot, "worktree", "add", "-b", branch, target, "HEAD"],
    { encoding: "utf8" },
  );
  if (result.status !== 0)
    throw new Error(
      `Could not create worktree for ${role.name}: ${result.stderr.trim()}`,
    );
  return target;
}

async function writeWorktreePrompt(
  cwd: string,
  run: Run,
  role: RunRole,
): Promise<void> {
  const promptFile = path.join(
    cwd,
    ".conduit",
    "assignments",
    `${run.id}-${role.name}.md`,
  );
  await mkdir(path.dirname(promptFile), { recursive: true });
  await writeFile(promptFile, role.prompt);
  const [command, args] = commandForRole(role, promptFile);
  role.command = command;
  role.args = args;
  role.worktreePromptFile = promptFile;
}

function terminate(child: ChildProcess): void {
  if (child.exitCode !== null || child.killed) return;
  try {
    if (process.platform !== "win32" && child.pid)
      process.kill(-child.pid, "SIGTERM");
    else child.kill("SIGTERM");
  } catch {
    // The child may already have exited before its process group is signalled.
  }
  setTimeout(() => {
    if (child.exitCode === null && !child.killed) {
      try {
        if (process.platform !== "win32" && child.pid)
          process.kill(-child.pid, "SIGKILL");
        else child.kill("SIGKILL");
      } catch {
        // Best effort: a completed child no longer needs to be killed.
      }
    }
  }, 3000).unref();
}

function changedFiles(cwd: string): string[] {
  const result: SpawnSyncReturns<string> = spawnSync(
    "git",
    ["-C", cwd, "diff", "--name-only"],
    {
      encoding: "utf8",
    },
  );
  return result.status === 0
    ? result.stdout.trim().split("\n").filter(Boolean)
    : [];
}

interface ChangedFileStat {
  file: string;
  added: number;
  removed: number;
}

function changedSummary(cwd: string): {
  files: ChangedFileStat[];
  added: number;
  removed: number;
  preview: string;
} {
  const result: SpawnSyncReturns<string> = spawnSync(
    "git",
    ["-C", cwd, "diff", "--numstat"],
    {
      encoding: "utf8",
    },
  );
  const stats = result.status === 0 ? result.stdout.trim() : "";
  const files: ChangedFileStat[] = stats
    ? stats.split("\n").map((line) => {
        const [added, removed, file] = line.split("\t");
        return {
          file,
          added: Number(added) || 0,
          removed: Number(removed) || 0,
        };
      })
    : [];
  const added = files.reduce((total, file) => total + file.added, 0);
  const removed = files.reduce((total, file) => total + file.removed, 0);
  const preview = files
    .slice(0, 2)
    .map(({ file, added: plus, removed: minus }) => {
      const diffResult: SpawnSyncReturns<string> = spawnSync(
        "git",
        ["-C", cwd, "diff", "--unified=1", "--", file],
        { encoding: "utf8" },
      );
      const diff = diffResult.status === 0 ? diffResult.stdout : "";
      const lines = diff
        .split("\n")
        .filter(
          (line) =>
            !line.startsWith("diff --git") &&
            !line.startsWith("index ") &&
            !line.startsWith("--- ") &&
            !line.startsWith("+++ "),
        )
        .slice(0, 8);
      return `\u2514 ${file} (+${plus} -${minus})\n${lines.map((line) => `  ${line}`).join("\n")}`;
    })
    .join("\n\n");
  return { files, added, removed, preview };
}

function runProcess(
  role: RunRole,
  cwd: string,
  logFile: string,
  onProgress: (message: string) => void,
  onChange: (event: { summary: string; preview: string }) => void,
  signal?: AbortSignal,
): Promise<RunResult> {
  return new Promise((resolve) => {
    const child = spawn(role.command, role.args, {
      cwd,
      stdio: ["ignore", "pipe", "pipe"],
      detached: process.platform !== "win32",
    });
    let output = "";
    let cancelled = false;
    let previousFiles = "";
    const cancel = () => {
      cancelled = true;
      onProgress(`${role.name}: cancelling`);
      terminate(child);
    };
    if (signal?.aborted) cancel();
    signal?.addEventListener("abort", cancel, { once: true });
    onProgress(`${role.name}: agent started`);
    const changeWatcher = setInterval(() => {
      const change = changedSummary(cwd);
      const fingerprint = change.files
        .map((file) => `${file.file}:${file.added}:${file.removed}`)
        .join("\n");
      if (fingerprint && fingerprint !== previousFiles) {
        previousFiles = fingerprint;
        const summary = `${role.name}: edited ${change.files.length} file${change.files.length === 1 ? "" : "s"} (+${change.added} -${change.removed})`;
        onProgress(summary);
        onChange({ summary, preview: change.preview });
      }
    }, 800);
    changeWatcher.unref();
    child.stdout?.on("data", (chunk: Buffer | string) => {
      output += chunk;
      onProgress(`${role.name}: working`);
    });
    child.stderr?.on("data", (chunk: Buffer | string) => {
      output += chunk;
      onProgress(`${role.name}: working`);
    });
    child.on("error", (error: Error) =>
      resolve({
        role: role.name,
        status: "failed",
        error: error.message,
        output,
      }),
    );
    child.on("close", async (code: number | null) => {
      await writeFile(logFile, output);
      clearInterval(changeWatcher);
      signal?.removeEventListener("abort", cancel);
      const finalChange = changedSummary(cwd);
      const finalFingerprint = finalChange.files
        .map((file) => `${file.file}:${file.added}:${file.removed}`)
        .join("\n");
      if (finalFingerprint && finalFingerprint !== previousFiles) {
        const summary = `${role.name}: edited ${finalChange.files.length} file${finalChange.files.length === 1 ? "" : "s"} (+${finalChange.added} -${finalChange.removed})`;
        onChange({ summary, preview: finalChange.preview });
      }
      const status: RunResult["status"] = cancelled
        ? "cancelled"
        : code === 0
          ? "completed"
          : "failed";
      const files = changedFiles(cwd);
      onProgress(
        `${role.name}: ${status}${files.length ? ` \u00b7 ${files.length} file${files.length === 1 ? "" : "s"} changed` : ""}`,
      );
      resolve({
        role: role.name,
        status,
        exitCode: code ?? undefined,
        output,
        files,
      });
    });
  });
}

export async function executeRun({
  projectRoot,
  run,
  runDir,
  dryRun = true,
  onProgress = () => {},
  onChange = () => {},
  signal,
}: {
  projectRoot: string;
  run: Run;
  runDir: string;
  dryRun?: boolean;
  onProgress?: (message: string) => void;
  onChange?: (event: { summary: string; preview: string }) => void;
  signal?: AbortSignal;
}): Promise<RunResult[]> {
  if (dryRun)
    return run.roles.map((role) => ({
      role: role.name,
      status: "dry-run" as const,
      command: [role.command, ...role.args],
    }));
  const launches = run.roles.map(async (role): Promise<RunResult> => {
    onProgress(
      `${role.name}: preparing ${role.readOnly ? "project workspace" : "isolated worktree"}`,
    );
    const cwd = role.readOnly
      ? projectRoot
      : addWorktree(projectRoot, run, role);
    role.worktree = role.readOnly ? undefined : cwd;
    if (!role.readOnly) await writeWorktreePrompt(cwd, run, role);
    return runProcess(
      role,
      cwd,
      path.join(runDir, `${role.name}.log`),
      onProgress,
      onChange,
      signal,
    );
  });
  const results = await Promise.all(launches);
  run.status = results.some((result) => result.status === "cancelled")
    ? "cancelled"
    : results.every((result) => result.status === "completed")
      ? "completed"
      : "failed";
  for (const role of run.roles)
    role.status =
      (results.find((result) => result.role === role.name)
        ?.status as RunRole["status"]) ?? "failed";
  await writeFile(path.join(runDir, "run.json"), JSON.stringify(run, null, 2));
  await writeFile(
    path.join(runDir, "results.json"),
    JSON.stringify(results, null, 2),
  );
  return results;
}

export async function latestRuns(
  projectRoot: string,
  config: Config,
): Promise<Run[]> {
  const dir = path.join(projectRoot, config.stateDir, "runs");
  const entries = await readdir(dir, { withFileTypes: true }).catch(
    () => [] as import("node:fs").Dirent[],
  );
  const runs: Run[] = [];
  for (const entry of entries.filter((item) => item.isDirectory())) {
    runs.push(
      JSON.parse(
        await readFile(path.join(dir, entry.name, "run.json"), "utf8"),
      ),
    );
  }
  return runs.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}
