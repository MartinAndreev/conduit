import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { mkdirSync, readdirSync, symlinkSync } from "node:fs";
import type { Dirent } from "node:fs";
import path from "node:path";
import { spawn, spawnSync } from "node:child_process";
import type { ChildProcess, SpawnSyncReturns } from "node:child_process";
import { resolveSkill } from "../../roles/repositories/skill-resolver.js";
import type { Config } from "../../configuration/types/config.js";
import type {
  Run,
  RunRole,
  RunResult,
  TerminalRunStatus,
} from "../types/run.js";
import type { RunnerEvent } from "../types/runner-events.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunProcessRegistry } from "./run-process-registry.js";
import { localSpecKitRoleContract } from "@domains/features/providers/local-spec-kit-role-contract.js";
import { agentResponseContractPrompt } from "../assets/agent-response-contract.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import {
  configureFinalOutputCapture,
  runnerAdapter,
  supportedRunners,
} from "@system/runners/registry.js";
import { parseAgentResponseV1 } from "../validation/agent-response-validator.js";
import {
  collectOwnershipWarnings,
  roleKindForRole,
  validateAgentResponseForAssignment,
} from "../validation/agent-semantic-validator.js";
import { createAgentAssignmentV1 } from "../factories/agent-assignment-factory.js";
import { validateAgentAssignmentV1 } from "../validation/agent-assignment-validator.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import { FileConduitResultRecordRepository } from "./file-conduit-result-record-repository.js";
import { RunnerEventProvenance } from "../enums/runner-event-provenance.js";
import { FileWorktreeLifecycleRepository } from "./file-worktree-lifecycle-repository.js";
import {
  ensureConduitStateGitIgnored,
  ensureWorktreeRootGitIgnored,
} from "@system/storage/factories/gitignore.js";
import {
  isUntrackedArtifactPath,
  untrackedArtifactGitExcludes,
} from "../helpers/dependency-tree-paths.js";

const databaseEnvironmentKey =
  /^(?:TURSO_|LIBSQL_|DATABASE_(?:URL|TOKEN)$|CONDUIT_DB)/i;
const dependencyDirectoryNames = new Set(["node_modules", "vendor"]);

export function agentProcessEnvironment(
  environment: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv {
  return Object.fromEntries(
    Object.entries(environment).filter(
      ([key]) => !databaseEnvironmentKey.test(key),
    ),
  );
}

export function commandForRole(
  role: {
    runner: string;
    model?: string;
    effort?: import("../../configuration/types/config.js").RoleReasoningEffort;
  },
  promptFile: string,
): [string, string[]] {
  const adapter = runnerAdapter(role.runner);
  if (!adapter)
    throw new Error(
      `Unsupported runner: ${role.runner}. Supported runners: ${supportedRunners().join(", ")}.`,
    );
  const args = [...adapter.buildArgs(promptFile, role.model)];
  if (role.effort && args.length) {
    const last = args[args.length - 1];
    args[args.length - 1] =
      `${last} Requested reasoning effort: ${role.effort}.`;
  }
  return [adapter.command, args];
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

async function featurePacketSnapshot(
  projectRoot: string,
  specsDirectory: string,
  featureId: string,
): Promise<string> {
  const specsRoot = path.resolve(projectRoot, specsDirectory);
  const featureDirectories = await readdir(specsRoot, {
    withFileTypes: true,
  }).catch(() => []);
  const featureDirectory = featureDirectories
    .filter(
      (entry) =>
        entry.isDirectory() &&
        (entry.name === featureId || entry.name.startsWith(`${featureId}-`)),
    )
    .sort((left, right) => left.name.localeCompare(right.name))[0];
  if (!featureDirectory) {
    return "The approved feature packet was not found when this assignment was planned.";
  }

  const root = path.join(specsRoot, featureDirectory.name);
  const files: string[] = [];
  const visit = async (directory: string): Promise<void> => {
    const entries = await readdir(directory, { withFileTypes: true });
    for (const entry of entries.sort((left, right) =>
      left.name.localeCompare(right.name),
    )) {
      const entryPath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        await visit(entryPath);
      } else if (entry.isFile()) {
        files.push(entryPath);
      }
    }
  };
  await visit(root);

  const sections: string[] = [];
  for (const file of files) {
    const content = await readFile(file, "utf8").catch(() => "");
    if (!content || content.includes("\0")) {
      continue;
    }
    sections.push(
      `## ${path.relative(projectRoot, file)}\n\n${content.trim()}`,
    );
  }
  return sections.join("\n\n---\n\n");
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
  const stateDirectory = path.resolve(projectRoot, config.stateDir);
  const configuredRoot =
    !config.worktreeRoot || config.worktreeRoot === "../.conduit-worktrees"
      ? path.join(config.stateDir, "worktrees")
      : config.worktreeRoot;
  const configuredWorktreeRoot = path.resolve(projectRoot, configuredRoot);
  await ensureConduitStateGitIgnored(stateDirectory);
  await ensureWorktreeRootGitIgnored(configuredWorktreeRoot);
  await cleanupExpiredWorktrees(
    projectRoot,
    stateDirectory,
    config.worktreeRetentionDays ?? 7,
  );
  await cleanupExpiredRunDiagnostics(
    stateDirectory,
    config.runDiagnosticsRetentionDays ?? 30,
  );
  await mkdir(runDir, { recursive: true });
  const packetSnapshot = await featurePacketSnapshot(
    projectRoot,
    config.specsDir,
    featureId,
  );
  const relativeStateDirectory = path.relative(projectRoot, stateDirectory);
  const forbiddenPaths = [
    ".git",
    ".conduit",
    ...(relativeStateDirectory &&
    relativeStateDirectory !== "." &&
    !relativeStateDirectory.startsWith(`..${path.sep}`) &&
    relativeStateDirectory !== ".."
      ? [relativeStateDirectory]
      : []),
  ].filter((value, index, values) => values.indexOf(value) === index);
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
    }).catch(() => ({
      source: role.skill.source,
      content: "No project role guidance was loaded.",
      verified: false,
    }));
    const promptFile = path.join(runDir, `${name}-assignment.json`);
    const contextFile = path.join(runDir, `${name}-context.md`);
    const roleDependencies = role.dependsOn ?? [];
    const context = redactSecrets(
      `${localSpecKitRoleContract(name, role.effort)}\n\n# Project role guidance (advisory)\n\n${skill.content}\n\n# Assignment (authoritative)\n\nFeature: ${featureId}\nRead the approved feature packet snapshot below before changing code.\nOwned paths: ${(role.owns ?? ["none defined"]).join(", ")}\nForbidden paths: ${forbiddenPaths.join(", ")}\nRun dependencies: ${roleDependencies.length ? roleDependencies.join(", ") : "none"}\nStay within assigned ownership when practical. Report necessary cross-ownership changes explicitly for integration review, and never modify forbidden paths.\nReport tests run and unresolved integration risks.\n\n${agentResponseContractPrompt()}\n\nThe system role contract and assignment take precedence over project role guidance.\n\n# Approved feature packet snapshot (read-only)\n\n${packetSnapshot}`,
    );
    const roleKind = roleKindForRole(name, role.roleKind);
    const assignment = createAgentAssignmentV1({
      assignmentId: `${runId}:${name}`,
      role: name,
      roleKind,
      objective: `Complete only the approved ${name} work for feature ${featureId}. Read the complete packet snapshot in the context reference before acting and return exactly one AgentResponseV1 object.`,
      ownedPaths: role.owns ?? [],
      forbiddenPaths,
      dependencies: roleDependencies,
      contextReferences: [path.relative(projectRoot, contextFile)],
      acceptanceCriteria: [
        "Satisfy the approved acceptance criteria in the referenced packet snapshot.",
        "Satisfy the approved test cases in the referenced packet snapshot.",
      ],
      contracts: [path.relative(projectRoot, contextFile)],
      expectedCapabilities: [
        role.readOnly ? "repository-read" : "workspace-write",
      ],
    });
    const assignmentValidation = validateAgentAssignmentV1(assignment);
    if (!assignmentValidation.valid)
      throw new Error(
        `Invalid assignment for ${name}: ${assignmentValidation.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
      );
    const prompt = `${JSON.stringify(assignment, null, 2)}\n`;
    await writeFile(contextFile, context);
    await writeFile(promptFile, prompt);
    const [command, args] = commandForRole(role, promptFile);
    roles.push({
      name,
      runner: role.runner,
      model: role.model,
      effort: role.effort,
      readOnly: Boolean(role.readOnly),
      owns: role.owns ?? [],
      dependsOn: roleDependencies,
      promptFile,
      prompt,
      context,
      contextFile,
      command,
      args,
      skillSource: skill.source,
      status: "planned",
      finalOutputFile: path.join(runDir, `${name}-agent-response.json`),
      assignment,
    });
  }
  assertDistinctOwnership(roles);
  const run: Run = {
    id: runId,
    featureId,
    status: "planned",
    createdAt: new Date().toISOString(),
    roles,
    stateDirectory,
    worktreeRoot: configuredWorktreeRoot,
    worktreeRetentionDays: config.worktreeRetentionDays ?? 7,
    runDiagnosticsRetentionDays: config.runDiagnosticsRetentionDays ?? 30,
  };
  return { run, runDir };
}

function worktreePath(projectRoot: string, run: Run, role: RunRole): string {
  return path.join(
    run.worktreeRoot ?? path.join(projectRoot, ".conduit", "worktrees"),
    path.basename(projectRoot),
    run.id,
    role.name,
  );
}

export async function cleanupExpiredWorktrees(
  projectRoot: string,
  stateDirectory: string,
  retentionDays: number,
): Promise<void> {
  const repository = new FileWorktreeLifecycleRepository(stateDirectory);
  const cutoff = new Date(
    Date.now() - Math.max(0, retentionDays) * 24 * 60 * 60 * 1_000,
  );
  for (const record of await repository.listExpired(cutoff)) {
    let removed = true;
    for (const worktree of record.worktrees) {
      const result = spawnSync(
        "git",
        ["-C", projectRoot, "worktree", "remove", "--force", worktree],
        { encoding: "utf8" },
      );
      if (
        result.status !== 0 &&
        !result.stderr.includes("is not a working tree")
      ) {
        removed = false;
      }
    }
    if (removed) await repository.remove(record.runId);
  }
}

export async function cleanupExpiredRunDiagnostics(
  stateDirectory: string,
  retentionDays: number,
): Promise<void> {
  const runsDirectory = path.join(stateDirectory, "runs");
  const entries = await readdir(runsDirectory, { withFileTypes: true }).catch(
    () => [],
  );
  const cutoff = Date.now() - Math.max(0, retentionDays) * 24 * 60 * 60 * 1_000;
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const runDirectory = path.join(runsDirectory, entry.name);
    const terminal = await readFile(
      path.join(runDirectory, "terminal.json"),
      "utf8",
    )
      .then((content) => JSON.parse(content) as { completedAt: string })
      .catch(() => undefined);
    if (!terminal || new Date(terminal.completedAt).getTime() > cutoff) {
      continue;
    }
    const artifacts = await readdir(runDirectory, {
      withFileTypes: true,
    }).catch(() => []);
    for (const artifact of artifacts) {
      if (!artifact.isFile()) continue;
      if (
        artifact.name.endsWith(".log") ||
        artifact.name.endsWith("-context.md") ||
        artifact.name.endsWith("-assignment.json") ||
        artifact.name.endsWith("-agent-response.json")
      ) {
        await rm(path.join(runDirectory, artifact.name), { force: true });
      }
    }
  }
}

function dependencyTreePaths(
  projectRoot: string,
  excludedDirectories: readonly string[],
): string[] {
  const dependencyTrees: string[] = [];
  const exclusions = excludedDirectories.map((directory) =>
    path.resolve(directory),
  );
  const visit = (directory: string): void => {
    const resolvedDirectory = path.resolve(directory);
    if (
      exclusions.some(
        (excluded) =>
          resolvedDirectory === excluded ||
          resolvedDirectory.startsWith(`${excluded}${path.sep}`),
      )
    ) {
      return;
    }
    let entries: Dirent[];
    try {
      entries = readdirSync(directory, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      if (!entry.isDirectory()) {
        continue;
      }
      const entryPath = path.join(directory, entry.name);
      if (dependencyDirectoryNames.has(entry.name)) {
        dependencyTrees.push(entryPath);
        continue;
      }
      if (
        entry.name === ".git" ||
        entry.name === ".conduit" ||
        entry.name === "dist"
      ) {
        continue;
      }
      visit(entryPath);
    }
  };
  visit(projectRoot);
  return dependencyTrees;
}

function linkDependencyTrees(
  projectRoot: string,
  worktree: string,
  excludedDirectories: readonly string[],
): void {
  for (const source of dependencyTreePaths(projectRoot, excludedDirectories)) {
    const relativePath = path.relative(projectRoot, source);
    const destination = path.join(worktree, relativePath);
    mkdirSync(path.dirname(destination), { recursive: true });
    symlinkSync(
      source,
      destination,
      process.platform === "win32" ? "junction" : "dir",
    );
  }
}

function addWorktree(projectRoot: string, run: Run, role: RunRole): string {
  // A newly initialized repository can have an unborn HEAD. Git cannot make
  // a worktree from it, but roles can still work safely in the project root
  // when their configured ownership does not overlap.
  if (!repositoryHasHead(projectRoot)) return projectRoot;
  const target = worktreePath(projectRoot, run, role);
  const branch = `conduit/${run.id}/${role.name}`;
  const disabledHooksDirectory = path.join(
    run.stateDirectory ?? path.join(projectRoot, ".conduit"),
    "hooks",
    "disabled",
  );
  mkdirSync(disabledHooksDirectory, { recursive: true });
  const result: SpawnSyncReturns<string> = spawnSync(
    "git",
    [
      "-c",
      `core.hooksPath=${disabledHooksDirectory}`,
      "-C",
      projectRoot,
      "worktree",
      "add",
      "-b",
      branch,
      target,
      "HEAD",
    ],
    { encoding: "utf8" },
  );
  if (result.status !== 0)
    throw new Error(
      `Could not create worktree for ${role.name}: ${result.stderr.trim()}`,
    );
  try {
    linkDependencyTrees(
      projectRoot,
      target,
      [target, run.stateDirectory, run.worktreeRoot].filter(
        (directory): directory is string => Boolean(directory),
      ),
    );
  } catch (cause) {
    spawnSync(
      "git",
      ["-C", projectRoot, "worktree", "remove", "--force", target],
      { encoding: "utf8" },
    );
    throw new Error(
      `Could not provision dependencies for ${role.name}: ${cause instanceof Error ? cause.message : String(cause)}`,
      { cause },
    );
  }
  return target;
}

function repositoryHasHead(projectRoot: string): boolean {
  return (
    spawnSync("git", ["-C", projectRoot, "rev-parse", "--verify", "HEAD"], {
      encoding: "utf8",
    }).status === 0
  );
}

async function writeWorktreePrompt(
  cwd: string,
  run: Run,
  role: RunRole,
): Promise<void> {
  const assignmentDir = path.join(cwd, ".conduit", "assignments");
  const promptFile = path.join(
    assignmentDir,
    `${run.id}-${role.name}-assignment.json`,
  );
  const contextFile = path.join(
    assignmentDir,
    `${run.id}-${role.name}-context.md`,
  );
  await mkdir(path.dirname(promptFile), { recursive: true });
  if (!role.assignment)
    throw new Error(`Run role ${role.name} is missing AgentAssignmentV1.`);
  role.assignment = {
    ...role.assignment,
    contextReferences: [path.relative(cwd, contextFile)],
    contracts: [path.relative(cwd, contextFile)],
  };
  const validation = validateAgentAssignmentV1(role.assignment);
  if (!validation.valid)
    throw new Error(
      `Invalid worktree assignment for ${role.name}: ${validation.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
    );
  role.prompt = `${JSON.stringify(role.assignment, null, 2)}\n`;
  await writeFile(contextFile, role.context ?? "");
  await writeFile(promptFile, role.prompt);
  const [command, args] = commandForRole(role, promptFile);
  role.command = command;
  role.args = args;
  role.worktreePromptFile = promptFile;
  role.contextFile = contextFile;
  role.finalOutputFile = path.join(
    assignmentDir,
    `${run.id}-${role.name}-agent-response.json`,
  );
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

function roleResultStatus(
  cancelled: boolean,
  protocolCompleted: boolean,
): RunResult["status"] {
  if (cancelled) {
    return "cancelled";
  }
  return protocolCompleted ? "completed" : "failed";
}

function terminalRunStatus(results: readonly RunResult[]): TerminalRunStatus {
  if (results.some((result) => result.status === "cancelled")) {
    return "cancelled";
  }
  if (results.every((result) => result.status === "completed")) {
    return "completed";
  }
  return "failed";
}

function completionMessage(
  status: TerminalRunStatus,
  reviewerSelected: boolean,
): string {
  if (status !== "completed") {
    return `Run ${status}`;
  }
  if (reviewerSelected) {
    return "Flow finished: reviewer completed with no required fixes.";
  }
  return "Flow finished: all selected agents completed.";
}

function changedFiles(cwd: string): string[] {
  const tracked: SpawnSyncReturns<string> = spawnSync(
    "git",
    ["-C", cwd, "diff", "--name-only", "HEAD"],
    {
      encoding: "utf8",
    },
  );
  const untracked: SpawnSyncReturns<string> = spawnSync(
    "git",
    [
      "-C",
      cwd,
      "ls-files",
      "--others",
      "--exclude-standard",
      ...untrackedArtifactGitExcludes(),
    ],
    { encoding: "utf8" },
  );
  const files = [
    ...(tracked.status === 0 ? tracked.stdout.split("\n") : []),
    ...(untracked.status === 0
      ? untracked.stdout
          .split("\n")
          .filter((file) => !isUntrackedArtifactPath(file))
      : []),
  ];
  return [...new Set(files)]
    .filter(Boolean)
    .filter((file) => !file.startsWith(".conduit/"));
}

function changedFileFingerprints(cwd: string): ReadonlyMap<string, string> {
  return new Map(
    changedFiles(cwd).map((file) => {
      const diff = spawnSync(
        "git",
        ["-C", cwd, "diff", "--binary", "HEAD", "--", file],
        { encoding: "utf8" },
      );
      if (diff.status === 0 && diff.stdout) return [file, diff.stdout];

      const hash = spawnSync("git", ["-C", cwd, "hash-object", "--", file], {
        encoding: "utf8",
      });
      return [file, hash.status === 0 ? hash.stdout.trim() : "missing"];
    }),
  );
}

function filesChangedSince(
  cwd: string,
  baseline: ReadonlyMap<string, string>,
): string[] {
  const finalState = changedFileFingerprints(cwd);
  return [...finalState.entries()]
    .filter(([file, fingerprint]) => baseline.get(file) !== fingerprint)
    .map(([file]) => file);
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
  runId: string,
  cwd: string,
  logFile: string,
  onProgress: (message: string) => void,
  onChange: (event: { summary: string; preview: string }) => void,
  eventRepository?: RunEventRepository,
  processRegistry?: RunProcessRegistry,
  resultRepository?: ConduitResultRecordRepository,
  featureId?: string,
  signal?: AbortSignal,
): Promise<RunResult> {
  const emitEvent = async (event: RunnerEvent) => {
    emittedEvents.push(event);
    if (eventRepository) await eventRepository.append(event);
  };
  const emittedEvents: RunnerEvent[] = [];

  return new Promise((resolve) => {
    const initialChangedFiles = changedFileFingerprints(cwd);
    const args = configureFinalOutputCapture(
      role.runner,
      role.args,
      role.finalOutputFile,
    );
    const child = spawn(role.command, args, {
      cwd,
      stdio: ["ignore", "pipe", "pipe"],
      detached: process.platform !== "win32",
      env: agentProcessEnvironment(),
    });
    let output = "";
    let stdout = "";
    let rawStdout = "";
    let rawStderr = "";
    let runnerFailureMessage: string | undefined;
    const adapter = runnerAdapter(role.runner);
    const stdoutParser = adapter?.createOutputParser?.(runId, role.name);
    const stderrParser = adapter?.createOutputParser?.(runId, role.name);
    let cancelled = false;
    let previousFiles = "";
    const abortController = new AbortController();

    // Register process for cancellation
    if (processRegistry) {
      processRegistry.register({
        runId,
        roleId: role.name,
        process: child,
        abortController,
      });
    }

    // Emit starting lifecycle event
    void emitEvent({
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: "starting",
        message: `${role.name}: agent starting`,
      },
    });

    const cancel = () => {
      cancelled = true;
      onProgress(`${role.name}: cancelling`);
      terminate(child);
    };
    abortController.signal.addEventListener("abort", cancel, { once: true });
    if (signal?.aborted) cancel();
    signal?.addEventListener("abort", cancel, { once: true });
    onProgress(`${role.name}: agent started`);

    // Emit running lifecycle event
    void emitEvent({
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: "running",
        message: `${role.name}: agent started`,
      },
    });

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
        // Emit file-change events for each changed file
        for (const file of change.files) {
          void emitEvent({
            type: "file-change",
            provenance: RunnerEventProvenance.ConduitObserved,
            runId,
            roleId: role.name,
            timestamp: new Date().toISOString(),
            payload: {
              kind: "file-change",
              path: file.file,
              additions: file.added,
              deletions: file.removed,
            },
          });
        }
      }
    }, 800);
    changeWatcher.unref();
    child.stdout?.on("data", (chunk: Buffer | string) => {
      rawStdout += String(chunk);
      const sanitizedChunk = redactSecrets(String(chunk));
      const transcript = sanitizedChunk.trim();
      output += sanitizedChunk;
      stdout += sanitizedChunk;
      onProgress(
        transcript
          ? `${role.name}: ${transcript.replace(/\s+/g, " ").slice(0, 100)}`
          : `${role.name}: working`,
      );
      const parsedEvents = stdoutParser?.push(sanitizedChunk) ?? [];
      if (parsedEvents.length) {
        for (const event of parsedEvents) {
          if (event.type === "error" && event.payload.kind === "error") {
            runnerFailureMessage = event.payload.message;
          }
          void emitEvent(event);
        }
      } else {
        void emitEvent({
          type: "tool-output",
          provenance: RunnerEventProvenance.ConduitObserved,
          runId,
          roleId: role.name,
          timestamp: new Date().toISOString(),
          payload: {
            kind: "tool-output",
            tool: "runner stdout",
            output: transcript.slice(0, 4_000),
            truncated: transcript.length > 4_000,
          },
        });
      }
    });
    child.stderr?.on("data", (chunk: Buffer | string) => {
      rawStderr += String(chunk);
      const sanitizedChunk = redactSecrets(String(chunk));
      const transcript = sanitizedChunk.trim();
      output += sanitizedChunk;
      onProgress(
        transcript
          ? `${role.name}: ${transcript.replace(/\s+/g, " ").slice(0, 100)}`
          : `${role.name}: working`,
      );
      const parsedEvents = stderrParser?.push(sanitizedChunk) ?? [];
      if (parsedEvents.length) {
        for (const event of parsedEvents) {
          if (event.type === "error" && event.payload.kind === "error") {
            runnerFailureMessage = event.payload.message;
          }
          void emitEvent(event);
        }
      } else {
        void emitEvent({
          type: "tool-output",
          provenance: RunnerEventProvenance.ConduitObserved,
          runId,
          roleId: role.name,
          timestamp: new Date().toISOString(),
          payload: {
            kind: "tool-output",
            tool: "runner stderr",
            output: transcript.slice(0, 4_000),
            truncated: transcript.length > 4_000,
          },
        });
      }
    });
    child.on("error", (error: Error) => {
      void emitEvent({
        type: "error",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "error",
          code: "PROCESS_ERROR",
          message: redactSecrets(error.message),
          recoverable: false,
        },
      });
      if (processRegistry) processRegistry.remove(runId, role.name);
      resolve({
        role: role.name,
        status: "failed",
        error: redactSecrets(error.message),
        output,
        stdout,
      });
    });
    child.on("close", async (code: number | null) => {
      await writeFile(logFile, output);
      clearInterval(changeWatcher);
      signal?.removeEventListener("abort", cancel);
      abortController.signal.removeEventListener("abort", cancel);
      if (processRegistry) processRegistry.remove(runId, role.name);
      const finalChange = changedSummary(cwd);
      const finalFingerprint = finalChange.files
        .map((file) => `${file.file}:${file.added}:${file.removed}`)
        .join("\n");
      if (finalFingerprint && finalFingerprint !== previousFiles) {
        const summary = `${role.name}: edited ${finalChange.files.length} file${finalChange.files.length === 1 ? "" : "s"} (+${finalChange.added} -${finalChange.removed})`;
        onChange({ summary, preview: finalChange.preview });
        for (const file of finalChange.files) {
          void emitEvent({
            type: "file-change",
            provenance: RunnerEventProvenance.ConduitObserved,
            runId,
            roleId: role.name,
            timestamp: new Date().toISOString(),
            payload: {
              kind: "file-change",
              path: file.file,
              additions: file.added,
              deletions: file.removed,
            },
          });
        }
      }
      const files = filesChangedSince(cwd, initialChangedFiles);
      const finalParserEvents = [
        ...(stdoutParser?.flush() ?? []),
        ...(stderrParser?.flush() ?? []),
      ];
      for (const event of finalParserEvents) {
        if (event.type === "error" && event.payload.kind === "error") {
          runnerFailureMessage = event.payload.message;
        }
        void emitEvent(event);
      }
      const capturedFinal = role.finalOutputFile
        ? await readFile(role.finalOutputFile, "utf8").catch(() => "")
        : "";
      const rawStdoutParser = adapter?.createOutputParser?.(runId, role.name);
      const rawStderrParser = adapter?.createOutputParser?.(runId, role.name);
      rawStdoutParser?.push(rawStdout);
      rawStdoutParser?.flush();
      rawStderrParser?.push(rawStderr);
      rawStderrParser?.flush();
      const finalResponseRaw = (
        capturedFinal.trim() ||
        rawStdoutParser?.finalResponse ||
        rawStderrParser?.finalResponse ||
        rawStdout
      ).trim();
      const structural = finalResponseRaw
        ? parseAgentResponseV1(finalResponseRaw)
        : {
            valid: false,
            issues: [
              { path: "$", message: "missing AgentResponseV1 final response" },
            ],
          };
      const assignmentPolicy = {
        roleKind: role.assignment?.roleKind ?? roleKindForRole(role.name),
        ownedPaths: role.owns,
        forbiddenPaths: role.assignment?.forbiddenPaths ?? [],
        observedChangedFiles: files,
        readOnly: role.readOnly,
      };
      const semantic =
        structural.valid && structural.value
          ? validateAgentResponseForAssignment(
              structural.value,
              assignmentPolicy,
            )
          : structural;
      const ownershipWarnings =
        structural.valid && structural.value
          ? collectOwnershipWarnings(structural.value, assignmentPolicy)
          : [];
      if (role.finalOutputFile && capturedFinal)
        await writeFile(role.finalOutputFile, redactSecrets(capturedFinal));
      const protocolCompleted = Boolean(
        code === 0 &&
        structural.valid &&
        semantic.valid &&
        structural.value?.status === "completed",
      );
      const status = roleResultStatus(cancelled, protocolCompleted);
      if (runnerFailureMessage) {
        void emitEvent({
          type: "error",
          provenance: RunnerEventProvenance.ConduitObserved,
          runId,
          roleId: role.name,
          timestamp: new Date().toISOString(),
          payload: {
            kind: "error",
            code: "RUNNER_EXECUTION_FAILED",
            message: runnerFailureMessage,
            recoverable: false,
          },
        });
      } else {
        let validationFailure:
          { code: string; message: string; recoverable: boolean } | undefined;
        if (!structural.valid) {
          const failures = structural.issues
            .map((item) => `${item.path}: ${item.message}`)
            .join("; ");
          validationFailure = {
            code: "AGENT_PROTOCOL_INVALID",
            message: `The agent did not return a valid AgentResponseV1 object: ${failures}. Sanitized output: ${logFile}.`,
            recoverable: true,
          };
        } else if (structural.value?.status !== "completed") {
          const responseStatus = structural.value?.status ?? "missing";
          const summary = structural.value?.summary ?? "No summary provided.";
          validationFailure = {
            code: "AGENT_RESPONSE_INCOMPLETE",
            message: `The agent returned ${responseStatus}: ${summary}`,
            recoverable: true,
          };
        } else if (!semantic.valid) {
          const failures = semantic.issues
            .map((item) => `${item.path}: ${item.message}`)
            .join("; ");
          validationFailure = {
            code: "AGENT_RESPONSE_INVALID",
            message: `The agent response violated its assignment policy: ${failures}. Sanitized output: ${logFile}.`,
            recoverable: true,
          };
        }
        if (validationFailure) {
          await emitEvent({
            type: "error",
            provenance: RunnerEventProvenance.ConduitObserved,
            runId,
            roleId: role.name,
            timestamp: new Date().toISOString(),
            payload: {
              kind: "error",
              ...validationFailure,
            },
          });
        }
      }
      onProgress(
        `${role.name}: ${status}${files.length ? ` · ${files.length} file${files.length === 1 ? "" : "s"} changed` : ""}`,
      );

      // Emit completion lifecycle event
      const lifecycleState =
        status === "completed"
          ? "completed"
          : status === "cancelled"
            ? "cancelled"
            : "failed";
      await emitEvent({
        type: "lifecycle",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "lifecycle",
          state: lifecycleState,
          message: `${role.name}: ${status}`,
        },
      });

      // Emit result event
      await emitEvent({
        type: "result",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "result",
          exitCode: code ?? -1,
          files,
          summary: `${role.name}: ${status}`,
        },
      });

      const resultRecord =
        structural.valid && structural.value
          ? {
              recordVersion: "1.0" as const,
              runId,
              featureId: featureId ?? "unknown",
              taskId: null,
              assignmentId:
                role.assignment?.assignmentId ?? `${runId}:${role.name}`,
              role: role.name,
              runner: role.runner,
              model: role.model ?? null,
              receivedAt: new Date().toISOString(),
              process: {
                exitCode: code ?? -1,
                acceptable: code === 0 && !cancelled,
                cancelled,
              },
              observedChangedFiles: files,
              conduitObservedEvents: emittedEvents.filter(
                (event) =>
                  event.provenance === RunnerEventProvenance.ConduitObserved,
              ),
              runnerReportedEvents: emittedEvents.filter(
                (event) =>
                  event.provenance === RunnerEventProvenance.RunnerReported,
              ),
              agentClaimedEvents: emittedEvents.filter(
                (event) =>
                  event.provenance === RunnerEventProvenance.AgentClaimed,
              ),
              protocolValidation: {
                valid: structural.valid,
                issues: structural.issues,
              },
              semanticValidation: {
                valid: semantic.valid,
                issues: semantic.issues,
              },
              ownershipWarnings,
              response: structural.value,
            }
          : undefined;
      if (resultRecord) await resultRepository?.save(resultRecord);

      resolve({
        role: role.name,
        status,
        exitCode: code ?? undefined,
        output,
        stdout,
        files,
        resultRecord,
      });
    });
  });
}

function roleExecutionGroups(roles: RunRole[]): RunRole[][] {
  const selected = new Map(roles.map((role) => [role.name, role]));
  const pending = new Set(roles.map((role) => role.name));
  const groups: RunRole[][] = [];
  while (pending.size) {
    const ready = [...pending]
      .map((name) => selected.get(name)!)
      .filter((role) =>
        role.dependsOn.every(
          (dependency) => !selected.has(dependency) || !pending.has(dependency),
        ),
      );
    if (!ready.length) {
      throw new Error(
        `Role dependency cycle detected among: ${[...pending].sort().join(", ")}.`,
      );
    }
    groups.push(ready);
    for (const role of ready) pending.delete(role.name);
  }
  return groups;
}

export async function executeRun({
  projectRoot,
  run,
  runDir,
  dryRun = true,
  onProgress = () => {},
  onChange = () => {},
  onRoleWorkspaceReady,
  signal,
  eventRepository,
  processRegistry,
}: {
  projectRoot: string;
  run: Run;
  runDir: string;
  dryRun?: boolean;
  onProgress?: (message: string) => void;
  onChange?: (event: { summary: string; preview: string }) => void;
  onRoleWorkspaceReady?: () => Promise<void>;
  signal?: AbortSignal;
  eventRepository?: RunEventRepository;
  processRegistry?: RunProcessRegistry;
}): Promise<RunResult[]> {
  const resultRepository = new FileConduitResultRecordRepository(
    path.dirname(runDir),
  );
  // Emit system-level starting event
  if (eventRepository) {
    await eventRepository.append({
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: run.id,
      roleId: "system",
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: "starting",
        message: "Run starting",
      },
    });
  }

  if (dryRun)
    return run.roles.map((role) => ({
      role: role.name,
      status: "dry-run" as const,
      command: [role.command, ...role.args],
    }));
  const launchRole = async (role: RunRole): Promise<RunResult> => {
    onProgress(
      `${role.name}: preparing ${role.readOnly ? "project workspace" : "isolated worktree"}`,
    );
    const cwd = role.readOnly
      ? projectRoot
      : addWorktree(projectRoot, run, role);
    role.worktree = role.readOnly ? undefined : cwd;
    await onRoleWorkspaceReady?.();
    if (!role.readOnly) await writeWorktreePrompt(cwd, run, role);
    return runProcess(
      role,
      run.id,
      cwd,
      path.join(runDir, `${role.name}.log`),
      onProgress,
      onChange,
      eventRepository,
      processRegistry,
      resultRepository,
      run.featureId,
      signal,
    );
  };
  const launchRoleSafely = async (role: RunRole): Promise<RunResult> => {
    try {
      return await launchRole(role);
    } catch (cause) {
      const message = redactSecrets(
        cause instanceof Error ? cause.message : String(cause),
      );
      await eventRepository?.append({
        type: "error",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId: run.id,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "error",
          code: "ROLE_LAUNCH_FAILED",
          message,
          recoverable: false,
        },
      });
      await eventRepository?.append({
        type: "lifecycle",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId: run.id,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "lifecycle",
          state: "failed",
          message: `${role.name}: failed to start`,
        },
      });
      return { role: role.name, status: "failed", error: message };
    }
  };
  const results: RunResult[] = [];
  const terminalByRole = new Map<string, RunResult>();
  const requiresSharedWorkspace = !repositoryHasHead(projectRoot);
  for (const group of roleExecutionGroups(run.roles)) {
    const blocked = group.filter((role) =>
      role.dependsOn.some((dependency) =>
        terminalByRole.has(dependency)
          ? terminalByRole.get(dependency)?.status !== "completed"
          : false,
      ),
    );
    for (const role of blocked) {
      const failedDependencies = role.dependsOn.filter(
        (dependency) =>
          terminalByRole.has(dependency) &&
          terminalByRole.get(dependency)?.status !== "completed",
      );
      const result: RunResult = {
        role: role.name,
        status: "failed",
        error: `Skipped because dependencies failed: ${failedDependencies.join(", ")}`,
      };
      results.push(result);
      terminalByRole.set(role.name, result);
      await eventRepository?.append({
        type: "lifecycle",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId: run.id,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "lifecycle",
          state: "failed",
          message: `${role.name}: skipped because dependencies failed`,
        },
      });
    }
    const runnable = group.filter((role) => !terminalByRole.has(role.name));
    const groupResults: RunResult[] = [];
    if (requiresSharedWorkspace) {
      for (const role of runnable) {
        groupResults.push(await launchRoleSafely(role));
      }
    } else {
      groupResults.push(...(await Promise.all(runnable.map(launchRoleSafely))));
    }
    for (const result of groupResults) {
      results.push(result);
      terminalByRole.set(result.role, result);
    }
  }
  const reviewerSelected = run.roles.some((role) => role.name === "reviewer");
  const terminalStatus = terminalRunStatus(results);
  run.status = terminalStatus;
  const worktrees = run.roles
    .map((role) => role.worktree)
    .filter((worktree): worktree is string => Boolean(worktree));
  if (run.stateDirectory && worktrees.length > 0) {
    await new FileWorktreeLifecycleRepository(run.stateDirectory).save({
      runId: run.id,
      status: terminalStatus,
      worktrees,
      completedAt: new Date().toISOString(),
    });
  }
  await writeFile(
    path.join(runDir, "terminal.json"),
    `${JSON.stringify({ status: run.status, completedAt: new Date().toISOString() }, null, 2)}\n`,
  );
  for (const role of run.roles)
    role.status =
      (results.find((result) => result.role === role.name)
        ?.status as RunRole["status"]) ?? "failed";
  const successfulRun = terminalStatus === "completed";
  const runCompletionMessage = completionMessage(
    terminalStatus,
    reviewerSelected,
  );

  // Emit system-level completion event
  if (eventRepository) {
    await eventRepository.append({
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: run.id,
      roleId: "system",
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: terminalStatus,
        message: runCompletionMessage,
      },
    });
    if (successfulRun) {
      await eventRepository.append({
        type: "activity",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId: run.id,
        roleId: "system",
        timestamp: new Date().toISOString(),
        payload: {
          kind: "activity",
          message: runCompletionMessage,
        },
      });
    }
  }

  return results;
}
