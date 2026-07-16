import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { mkdirSync, readdirSync, rmSync, symlinkSync } from "node:fs";
import type { Dirent } from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import type { SpawnSyncReturns } from "node:child_process";
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
import { runnerAdapter, supportedRunners } from "@system/runners/registry.js";
import {
  consumeCommunicationStream,
  createDefaultCommunicationProviders,
  selectCommunicationProvider,
} from "@system/communication/index.js";
import type { ConduitRuntimeEvent } from "@system/communication/types/runtime-event.js";
import {
  BoundedTranscriptWriter,
  cleanupTranscripts,
  defaultTranscriptRetentionPolicy,
} from "@system/communication/services/transcript-retention.js";
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
  const configuredWorktreeRoot = path.resolve(
    projectRoot,
    config.worktreeRoot ?? "../.conduit-worktrees",
  );
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
  await cleanupTranscripts(path.join(stateDirectory, "runs"), {
    ...defaultTranscriptRetentionPolicy,
    retentionDays: config.runDiagnosticsRetentionDays ?? 7,
  });
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
      contextReferences: [config.specsDir],
      acceptanceCriteria: [
        "Satisfy the approved acceptance criteria in the referenced packet snapshot.",
        "Satisfy the approved test cases in the referenced packet snapshot.",
      ],
      contracts: [config.specsDir],
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

function concealTrackedAgentState(worktree: string): void {
  const tracked = spawnSync(
    "git",
    ["-C", worktree, "ls-files", ".conduit", "state.db"],
    { encoding: "utf8" },
  );
  const paths =
    tracked.status === 0 ? tracked.stdout.split("\n").filter(Boolean) : [];
  if (!paths.length) return;
  const concealed = spawnSync(
    "git",
    ["-C", worktree, "update-index", "--skip-worktree", "--", ...paths],
    { encoding: "utf8" },
  );
  if (concealed.status !== 0)
    throw new Error(
      `Could not isolate tracked Conduit state: ${concealed.stderr.trim()}`,
    );
}

function addWorktree(projectRoot: string, run: Run, role: RunRole): string {
  if (!repositoryHasHead(projectRoot))
    throw new Error(
      "Agent isolation requires a committed Git HEAD before a run can start.",
    );
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
    concealTrackedAgentState(target);
    rmSync(path.join(target, ".conduit"), { recursive: true, force: true });
    rmSync(path.join(target, "state.db"), { force: true });
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

function integrateDependencyCommits(
  worktree: string,
  commits: readonly string[],
  disabledHooksDirectory: string,
): void {
  for (const commit of [...new Set(commits)]) {
    const result = spawnSync(
      "git",
      [
        "-c",
        `core.hooksPath=${disabledHooksDirectory}`,
        "-C",
        worktree,
        "cherry-pick",
        commit,
      ],
      { encoding: "utf8" },
    );
    if (result.status !== 0) {
      spawnSync("git", ["-C", worktree, "cherry-pick", "--abort"], {
        encoding: "utf8",
      });
      throw new Error(
        `Could not integrate dependency commit ${commit}: ${result.stderr.trim()}`,
      );
    }
  }
}

function commitRoleArtifacts(
  worktree: string,
  roleName: string,
  files: readonly string[],
  disabledHooksDirectory: string,
): string | undefined {
  if (!files.length) return undefined;
  const staged = spawnSync(
    "git",
    ["-C", worktree, "add", "-A", "--", ...files],
    { encoding: "utf8" },
  );
  if (staged.status !== 0)
    throw new Error(
      `Could not stage ${roleName} artifacts: ${staged.stderr.trim()}`,
    );
  const tree = spawnSync("git", ["-C", worktree, "write-tree"], {
    encoding: "utf8",
  });
  if (tree.status !== 0)
    throw new Error(`Could not write ${roleName} artifact tree.`);
  const parent = spawnSync("git", ["-C", worktree, "rev-parse", "HEAD"], {
    encoding: "utf8",
  });
  if (parent.status !== 0)
    throw new Error(`Could not resolve ${roleName} artifact parent.`);
  const committed = spawnSync(
    "git",
    [
      "-c",
      `core.hooksPath=${disabledHooksDirectory}`,
      "-c",
      "user.name=Conduit",
      "-c",
      "user.email=conduit@localhost",
      "-C",
      worktree,
      "commit-tree",
      tree.stdout.trim(),
      "-p",
      parent.stdout.trim(),
      "-m",
      `conduit: capture ${roleName} artifacts`,
    ],
    { encoding: "utf8" },
  );
  if (committed.status !== 0)
    throw new Error(
      `Could not commit ${roleName} artifacts: ${committed.stderr.trim()}`,
    );
  return committed.stdout.trim();
}

function repositoryHasHead(projectRoot: string): boolean {
  return (
    spawnSync("git", ["-C", projectRoot, "rev-parse", "--verify", "HEAD"], {
      encoding: "utf8",
    }).status === 0
  );
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

async function runProcess(
  role: RunRole,
  runId: string,
  cwd: string,
  logFile: string,
  onProgress: (message: string) => void,
  onChange: (event: { summary: string; preview: string }) => void,
  eventRepository?: RunEventRepository,
  processRegistry?: RunProcessRegistry,
  resultRepository?: ConduitResultRecordRepository,
  runtimeEventRepository?: import("../interfaces/runtime-event-repository.js").RuntimeEventRepository,
  communicationProviders?: readonly import("@system/communication/types/provider.js").AgentCommunicationProvider[],
  featureId?: string,
  signal?: AbortSignal,
): Promise<RunResult> {
  const emittedEvents: RunnerEvent[] = [];
  const emitEvent = async (event: RunnerEvent): Promise<void> => {
    emittedEvents.push(event);
    await eventRepository?.append(event);
  };
  const observedChangedFiles = changedFileFingerprints(cwd);
  const abortController = new AbortController();
  const providers =
    communicationProviders ?? createDefaultCommunicationProviders();
  let selected;
  try {
    selected = await selectCommunicationProvider(providers, role.runner);
  } catch (cause) {
    const message = redactSecrets(
      cause instanceof Error ? cause.message : String(cause),
    );
    await emitEvent({
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: { kind: "lifecycle", state: "unavailable", message },
    });
    return { role: role.name, status: "failed", error: message };
  }
  if (!role.assignment)
    return {
      role: role.name,
      status: "failed",
      error: "Role assignment is missing",
    };
  const session = await selected.provider.createSession({
    assignment: role.assignment,
    projectRoot: cwd,
    workspaceRoot: cwd,
    runner: role.runner,
    ...(role.model ? { model: role.model } : {}),
    ...(role.effort ? { effort: role.effort } : {}),
    signal: abortController.signal,
  });
  processRegistry?.register({ runId, roleId: role.name, abortController });
  const cancel = (): void => {
    abortController.abort();
    void session.cancel();
  };
  if (signal?.aborted) cancel();
  signal?.addEventListener("abort", cancel, { once: true });
  await emitEvent({
    type: "lifecycle",
    provenance: RunnerEventProvenance.ConduitObserved,
    runId,
    roleId: role.name,
    timestamp: new Date().toISOString(),
    payload: {
      kind: "lifecycle",
      state: "starting",
      message: `${role.name}: starting ${selected.inspection.capability.protocol}`,
    },
  });
  if (selected.inspection.degradedReason) {
    await emitEvent({
      type: "activity",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "activity",
        message: `Telemetry degraded: ${selected.inspection.degradedReason}`,
      },
    });
  }
  onProgress(
    `${role.name}: agent started (${selected.inspection.capability.protocol})`,
  );
  let previousFiles = "";
  const changeWatcher = setInterval(() => {
    const change = changedSummary(cwd);
    const fingerprint = change.files
      .map((file) => `${file.file}:${file.added}:${file.removed}`)
      .join("\n");
    if (!fingerprint || fingerprint === previousFiles) return;
    previousFiles = fingerprint;
    const summary = `${role.name}: edited ${change.files.length} file${change.files.length === 1 ? "" : "s"} (+${change.added} -${change.removed})`;
    onProgress(summary);
    onChange({ summary, preview: change.preview });
  }, 800);
  changeWatcher.unref();
  const diagnosticLines: string[] = [];
  const transcriptWriter = new BoundedTranscriptWriter(logFile);
  let runnerFailureMessage: string | undefined;
  const displayValue = (value: unknown, limit: number): string => {
    if (value === undefined || value === null) return "";
    const serialized =
      typeof value === "string"
        ? value
        : (JSON.stringify(value) ?? String(value));
    return serialized.slice(0, limit);
  };
  const runtimeToRunnerEvent = (
    event: ConduitRuntimeEvent,
  ): RunnerEvent | undefined => {
    const base = {
      provenance:
        event.provenance === "conduit-observed"
          ? RunnerEventProvenance.ConduitObserved
          : event.provenance === "agent-claimed"
            ? RunnerEventProvenance.AgentClaimed
            : RunnerEventProvenance.RunnerReported,
      runId,
      roleId: role.name,
      timestamp: event.receivedAt,
    } as const;
    const message = displayValue(
      event.payload.message ??
        event.payload.summary ??
        event.payload.state ??
        event.type,
      2_000,
    );
    if (
      event.type === "native-error" ||
      event.type === "warning" ||
      event.type === "dropped-events"
    ) {
      if (event.type === "native-error") runnerFailureMessage = message;
      return {
        ...base,
        type: "error",
        payload: {
          kind: "error",
          code:
            event.type === "native-error"
              ? "RUNNER_EXECUTION_FAILED"
              : event.type === "dropped-events"
                ? "TELEMETRY_DROPPED"
                : "RUNNER_WARNING",
          message,
          recoverable: event.type !== "native-error",
        },
      };
    }
    if (event.type === "tool-call" || event.type === "command") {
      const tool = displayValue(
        event.payload.tool ?? (event.type === "command" ? "shell" : "unknown"),
        200,
      );
      const args = displayValue(
        event.payload.command ?? event.payload.input ?? "",
        1_000,
      );
      if (event.payload.state === "completed" && event.payload.output)
        return {
          ...base,
          type: "tool-output",
          payload: {
            kind: "tool-output",
            tool,
            output: displayValue(event.payload.output, 4_000),
            truncated: displayValue(event.payload.output, 4_001).length > 4_000,
          },
        };
      return {
        ...base,
        type: "tool-call",
        payload: { kind: "tool-call", tool, ...(args ? { args } : {}) },
      };
    }
    if (event.type === "file-operation")
      return {
        ...base,
        type: "file-change",
        payload: {
          kind: "file-change",
          path: String(event.payload.path ?? ""),
          additions: Number(event.payload.additions ?? 0),
          deletions: Number(event.payload.deletions ?? 0),
        },
      };
    if (event.type === "final-response-candidate")
      return {
        ...base,
        type: "activity",
        payload: {
          kind: "activity",
          message: "Structured final response received",
        },
      };
    if (event.type === "process-outcome" || event.type === "worktree-change")
      return undefined;
    return {
      ...base,
      type: "activity",
      payload: { kind: "activity", message },
    };
  };
  let terminal;
  try {
    await session.start();
    await session.submit(role.assignment);
    await emitEvent({
      type: "lifecycle",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: "running",
        message: `${role.name}: assignment accepted`,
      },
    });
    terminal = await consumeCommunicationStream(
      session.events,
      async (runtimeEvent) => {
        await runtimeEventRepository?.append(runtimeEvent);
        const event = runtimeToRunnerEvent(runtimeEvent);
        const diagnostic = JSON.stringify(runtimeEvent);
        diagnosticLines.push(diagnostic);
        if (diagnosticLines.length > 2_000) diagnosticLines.shift();
        await transcriptWriter.append(`${diagnostic}\n`);
        if (event) {
          await emitEvent(event);
          if (event.payload.kind === "activity")
            onProgress(`${role.name}: ${event.payload.message.slice(0, 100)}`);
        }
      },
    );
  } catch (cause) {
    terminal = {
      status: abortController.signal.aborted
        ? ("cancelled" as const)
        : ("failed" as const),
      diagnostics: [
        redactSecrets(cause instanceof Error ? cause.message : String(cause)),
      ],
    };
  } finally {
    clearInterval(changeWatcher);
    signal?.removeEventListener("abort", cancel);
    processRegistry?.remove(runId, role.name);
    await session.close();
  }
  const files = filesChangedSince(cwd, observedChangedFiles);
  const finalResponseRaw = terminal.finalResponseCandidate?.trim() ?? "";
  const structural = finalResponseRaw
    ? parseAgentResponseV1(finalResponseRaw)
    : {
        valid: false as const,
        issues: [
          { path: "$", message: "missing AgentResponseV1 final response" },
        ],
      };
  const assignmentPolicy = {
    roleKind: role.assignment.roleKind,
    ownedPaths: role.owns,
    forbiddenPaths: role.assignment.forbiddenPaths,
    observedChangedFiles: files,
    readOnly: role.readOnly,
  };
  const semantic =
    structural.valid && structural.value
      ? validateAgentResponseForAssignment(structural.value, assignmentPolicy)
      : structural;
  const ownershipWarnings =
    structural.valid && structural.value
      ? collectOwnershipWarnings(structural.value, assignmentPolicy)
      : [];
  const cancelled = terminal.status === "cancelled";
  const protocolCompleted = Boolean(
    terminal.status === "completed" &&
    !runnerFailureMessage &&
    (terminal.exitCode === undefined || terminal.exitCode === 0) &&
    structural.valid &&
    semantic.valid &&
    structural.value?.status === "completed",
  );
  const status = roleResultStatus(cancelled, protocolCompleted);
  if (finalResponseRaw && role.finalOutputFile)
    await writeFile(role.finalOutputFile, redactSecrets(finalResponseRaw));
  if (!protocolCompleted && !runnerFailureMessage) {
    const message = !structural.valid
      ? `The agent did not return a valid AgentResponseV1 object: ${structural.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}.`
      : structural.value?.status !== "completed"
        ? `The agent returned ${structural.value?.status}: ${structural.value?.summary}`
        : `The agent response violated its assignment policy: ${semantic.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}.`;
    await emitEvent({
      type: "error",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "error",
        code: !structural.valid
          ? "AGENT_PROTOCOL_INVALID"
          : "AGENT_RESPONSE_INVALID",
        message,
        recoverable: true,
      },
    });
  }
  await emitEvent({
    type: "lifecycle",
    provenance: RunnerEventProvenance.ConduitObserved,
    runId,
    roleId: role.name,
    timestamp: new Date().toISOString(),
    payload: {
      kind: "lifecycle",
      state:
        status === "completed"
          ? "completed"
          : status === "cancelled"
            ? "cancelled"
            : "failed",
      message: `${role.name}: ${status}`,
    },
  });
  await emitEvent({
    type: "result",
    provenance: RunnerEventProvenance.ConduitObserved,
    runId,
    roleId: role.name,
    timestamp: new Date().toISOString(),
    payload: {
      kind: "result",
      exitCode: terminal.exitCode ?? -1,
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
          assignmentId: role.assignment.assignmentId,
          role: role.name,
          runner: role.runner,
          model: role.model ?? null,
          receivedAt: new Date().toISOString(),
          process: {
            exitCode: terminal.exitCode ?? -1,
            acceptable:
              terminal.status === "completed" && !runnerFailureMessage,
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
            (event) => event.provenance === RunnerEventProvenance.AgentClaimed,
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
  onProgress(
    `${role.name}: ${status}${files.length ? ` · ${files.length} files changed` : ""}`,
  );
  return {
    role: role.name,
    status,
    exitCode: terminal.exitCode,
    output: diagnosticLines.join("\n"),
    stdout: "",
    files,
    resultRecord,
  };
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
  resultRepository,
  runtimeEventRepository,
  communicationProviders,
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
  resultRepository?: ConduitResultRecordRepository;
  runtimeEventRepository?: import("../interfaces/runtime-event-repository.js").RuntimeEventRepository;
  communicationProviders?: readonly import("@system/communication/types/provider.js").AgentCommunicationProvider[];
}): Promise<RunResult[]> {
  const effectiveResultRepository =
    resultRepository ??
    new FileConduitResultRecordRepository(path.dirname(runDir));
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
    onProgress(`${role.name}: preparing isolated worktree`);
    const cwd = addWorktree(projectRoot, run, role);
    role.worktree = cwd;
    const disabledHooksDirectory = path.join(
      run.stateDirectory ?? path.join(projectRoot, ".conduit"),
      "hooks",
      "disabled",
    );
    const inheritedCommits = [
      ...new Set(
        role.dependsOn.flatMap(
          (dependency) =>
            run.roles.find((candidate) => candidate.name === dependency)
              ?.integrationCommits ?? [],
        ),
      ),
    ];
    integrateDependencyCommits(cwd, inheritedCommits, disabledHooksDirectory);
    role.integrationCommits = inheritedCommits;
    await onRoleWorkspaceReady?.();
    const result = await runProcess(
      role,
      run.id,
      cwd,
      path.join(runDir, `${role.name}.log`),
      onProgress,
      onChange,
      eventRepository,
      processRegistry,
      effectiveResultRepository,
      runtimeEventRepository,
      communicationProviders,
      run.featureId,
      signal,
    );
    if (result.status === "completed") {
      const commit = commitRoleArtifacts(
        cwd,
        role.name,
        result.files ?? [],
        disabledHooksDirectory,
      );
      role.integrationCommits = commit
        ? [...inheritedCommits, commit]
        : inheritedCommits;
    }
    return result;
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

  if (run.stateDirectory) {
    await cleanupTranscripts(path.join(run.stateDirectory, "runs"));
  }

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
