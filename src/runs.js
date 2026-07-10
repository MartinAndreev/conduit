import { mkdir, readFile, writeFile, readdir } from "node:fs/promises";
import path from "node:path";
import { spawn, spawnSync } from "node:child_process";
import { resolveSkill } from "./skills.js";

const runnerAdapters = {
  opencode: { command: "opencode", beforeModel: ["run"], afterModel: [] },
  codex: { command: "codex", beforeModel: ["exec"], afterModel: [] },
  pi: { command: "pi", beforeModel: [], afterModel: ["-p"] },
  kilo: { command: "kilo", beforeModel: ["run"], afterModel: [] },
};

export function commandForRole(role, promptFile) {
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

function pathsOverlap(left = [], right = []) {
  return left.some((a) =>
    right.some(
      (b) => a === b || a.startsWith(`${b}/`) || b.startsWith(`${a}/`),
    ),
  );
}

function assertDistinctOwnership(roles) {
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
}) {
  const runId = `${featureId}-${Date.now()}`;
  const runDir = path.join(projectRoot, config.stateDir, "runs", runId);
  await mkdir(runDir, { recursive: true });
  const roles = [];
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
  const run = {
    id: runId,
    featureId,
    status: "planned",
    createdAt: new Date().toISOString(),
    roles,
  };
  await writeFile(path.join(runDir, "run.json"), JSON.stringify(run, null, 2));
  return { run, runDir };
}

function worktreePath(projectRoot, run, role) {
  return path.join(
    path.dirname(projectRoot),
    ".conduit-worktrees",
    path.basename(projectRoot),
    run.id,
    role.name,
  );
}

function addWorktree(projectRoot, run, role) {
  const target = worktreePath(projectRoot, run, role);
  const branch = `conduit/${run.id}/${role.name}`;
  const result = spawnSync(
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

async function writeWorktreePrompt(cwd, run, role) {
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

function terminate(child) {
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

function changedFiles(cwd) {
  const result = spawnSync("git", ["-C", cwd, "diff", "--name-only"], {
    encoding: "utf8",
  });
  return result.status === 0
    ? result.stdout.trim().split("\n").filter(Boolean)
    : [];
}

function changedSummary(cwd) {
  const result = spawnSync("git", ["-C", cwd, "diff", "--numstat"], {
    encoding: "utf8",
  });
  const stats = result.status === 0 ? result.stdout.trim() : "";
  const files = stats
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
      const result = spawnSync(
        "git",
        ["-C", cwd, "diff", "--unified=1", "--", file],
        { encoding: "utf8" },
      );
      const diff = result.status === 0 ? result.stdout : "";
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
      return `└ ${file} (+${plus} -${minus})\n${lines.map((line) => `  ${line}`).join("\n")}`;
    })
    .join("\n\n");
  return { files, added, removed, preview };
}

function runProcess(role, cwd, logFile, onProgress, onChange, signal) {
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
    child.stdout.on("data", (chunk) => {
      output += chunk;
      onProgress(`${role.name}: working`);
    });
    child.stderr.on("data", (chunk) => {
      output += chunk;
      onProgress(`${role.name}: working`);
    });
    child.on("error", (error) =>
      resolve({
        role: role.name,
        status: "failed",
        error: error.message,
        output,
      }),
    );
    child.on("close", async (code) => {
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
      const status = cancelled
        ? "cancelled"
        : code === 0
          ? "completed"
          : "failed";
      const files = changedFiles(cwd);
      onProgress(
        `${role.name}: ${status}${files.length ? ` · ${files.length} file${files.length === 1 ? "" : "s"} changed` : ""}`,
      );
      resolve({ role: role.name, status, exitCode: code, output, files });
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
}) {
  if (dryRun)
    return run.roles.map((role) => ({
      role: role.name,
      status: "dry-run",
      command: [role.command, ...role.args],
    }));
  const launches = run.roles.map(async (role) => {
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
      results.find((result) => result.role === role.name)?.status ?? "failed";
  await writeFile(path.join(runDir, "run.json"), JSON.stringify(run, null, 2));
  await writeFile(
    path.join(runDir, "results.json"),
    JSON.stringify(results, null, 2),
  );
  return results;
}

export async function latestRuns(projectRoot, config) {
  const dir = path.join(projectRoot, config.stateDir, "runs");
  const entries = await readdir(dir, { withFileTypes: true }).catch(() => []);
  const runs = [];
  for (const entry of entries.filter((item) => item.isDirectory())) {
    runs.push(
      JSON.parse(
        await readFile(path.join(dir, entry.name, "run.json"), "utf8"),
      ),
    );
  }
  return runs.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}
