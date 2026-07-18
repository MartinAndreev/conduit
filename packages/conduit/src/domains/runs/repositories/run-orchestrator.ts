import { createHash } from "node:crypto";
import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import {
  constants as fsConstants,
  cpSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readdirSync,
  rmSync,
} from "node:fs";
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
import type {
  AgentAssignmentV1,
  AgentFindingV1,
  AgentResponseV1,
} from "../types/agent-protocol.js";
import { FileConduitResultRecordRepository } from "./file-conduit-result-record-repository.js";
import { RunnerEventProvenance } from "../enums/runner-event-provenance.js";
import { FileWorktreeLifecycleRepository } from "./file-worktree-lifecycle-repository.js";
import {
  ensureConduitStateGitIgnored,
  ensureWorktreeRootGitIgnored,
} from "@system/storage/factories/gitignore.js";
import { hashFeaturePackage } from "../../features/services/feature-package-hasher.js";
import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import type { RoleWorkspaceLeaseIdentity } from "../types/role-workspace.js";
import { RoleWorkspaceState } from "../enums/role-workspace-state.js";
import {
  assertDistinctRoleWorkspaceKeys,
  normalizeRoleWorkspaceKey,
  resolveRepositoryIdentity,
  roleWorkspaceSlotPath,
} from "../services/role-workspace-identity-service.js";
import { isDependencyTreePath } from "../helpers/dependency-tree-paths.js";
import { gitWorktreeRegistry } from "@system/git/services/git-worktree-registry-service.js";

const databaseEnvironmentKey =
  /^(?:TURSO_|LIBSQL_|DATABASE_(?:URL|TOKEN)$|CONDUIT_DB)/i;
const dependencyDirectoryNames = new Set(["node_modules", "vendor"]);
const maxReviewerCorrectionRounds = 2;
const maxAutomaticRoleRetries = 2;

function assignmentIdentityHash(assignment: AgentAssignmentV1): string {
  return createHash("sha256")
    .update(JSON.stringify(assignment), "utf8")
    .digest("hex");
}

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

function pathIsWithin(
  repositoryPath: string,
  configuredRoots: readonly string[],
): boolean {
  return configuredRoots.some(
    (root) =>
      root === "." ||
      repositoryPath === root ||
      repositoryPath.startsWith(`${root}/`),
  );
}

function actionableReviewFindings(
  response: AgentResponseV1,
): readonly AgentFindingV1[] {
  return response.findings.filter((finding) => finding.severity !== "info");
}

function reviewFingerprint(response: AgentResponseV1): string {
  return JSON.stringify(
    actionableReviewFindings(response)
      .map((finding) => ({
        severity: finding.severity,
        category: finding.category,
        message: finding.message,
        path: finding.path ?? null,
        line: finding.line ?? null,
      }))
      .sort((left, right) =>
        JSON.stringify(left).localeCompare(JSON.stringify(right)),
      ),
  );
}

function routeReviewFindings(
  response: AgentResponseV1,
  roles: readonly RunRole[],
  baseAssignments: ReadonlyMap<string, AgentAssignmentV1>,
): ReadonlyMap<RunRole, readonly AgentFindingV1[]> {
  const routed = new Map<RunRole, AgentFindingV1[]>();
  const findings = actionableReviewFindings(response);
  if (!findings.length)
    throw new Error("Reviewer requested changes without actionable findings.");
  for (const finding of findings) {
    if (!finding.path)
      throw new Error(
        "Reviewer finding is missing a repository-relative path.",
      );
    const owners = roles.filter((role) => {
      const assignment = baseAssignments.get(role.name);
      return Boolean(
        !role.readOnly &&
        assignment &&
        pathIsWithin(finding.path!, assignment.ownedPaths) &&
        !pathIsWithin(finding.path!, assignment.forbiddenPaths),
      );
    });
    if (owners.length !== 1)
      throw new Error(
        `Reviewer finding path ${finding.path} has ${owners.length === 0 ? "no writable owner" : "ambiguous writable ownership"}.`,
      );
    const owner = owners[0]!;
    routed.set(owner, [...(routed.get(owner) ?? []), finding]);
  }
  return routed;
}

function turnAssignmentId(originalAssignmentId: string, round: number): string {
  const suffix = `:review-feedback:${round}`;
  return `${originalAssignmentId.slice(0, 120 - suffix.length)}${suffix}`;
}

function validatedTurnAssignment(input: {
  original: AgentAssignmentV1;
  round: number;
  objective: string;
  additionalAcceptanceCriteria: readonly string[];
}): AgentAssignmentV1 {
  const assignment = createAgentAssignmentV1({
    assignmentId: turnAssignmentId(input.original.assignmentId, input.round),
    role: input.original.role,
    roleKind: input.original.roleKind,
    objective: input.objective,
    ownedPaths: input.original.ownedPaths,
    forbiddenPaths: input.original.forbiddenPaths,
    dependencies: input.original.dependencies,
    contextReferences: input.original.contextReferences,
    acceptanceCriteria: [
      ...input.original.acceptanceCriteria,
      ...input.additionalAcceptanceCriteria,
    ].slice(0, 100),
    contracts: input.original.contracts,
    requiredVerification: input.original.requiredVerification,
    expectedCapabilities: input.original.expectedCapabilities,
  });
  const validation = validateAgentAssignmentV1(assignment);
  if (!validation.valid)
    throw new Error(
      `Invalid review-feedback assignment: ${validation.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
    );
  return assignment;
}

function correctionAssignment(
  original: AgentAssignmentV1,
  findings: readonly AgentFindingV1[],
  round: number,
): AgentAssignmentV1 {
  const findingSummary = JSON.stringify(
    findings.map((finding) => ({
      path: finding.path,
      line: finding.line,
      message: finding.message,
      evidence: finding.evidence,
    })),
  ).slice(0, 3_000);
  return validatedTurnAssignment({
    original,
    round,
    objective:
      `Apply only reviewer correction round ${round} in the existing isolated workspace. Preserve the original assignment and address these structured findings: ${findingSummary}`.slice(
        0,
        4_000,
      ),
    additionalAcceptanceCriteria: findings.map((finding) =>
      `${finding.path}: ${finding.message}`.slice(0, 500),
    ),
  });
}

function reReviewAssignment(
  original: AgentAssignmentV1,
  round: number,
): AgentAssignmentV1 {
  return validatedTurnAssignment({
    original,
    round,
    objective: `Re-review correction round ${round} in the integrated read-only workspace. Approve only if every material finding is resolved and required evidence is available.`,
    additionalAcceptanceCriteria: [],
  });
}

function resumedAssignment(
  original: AgentAssignmentV1,
  attempt: number,
  failureFeedback?: string,
): AgentAssignmentV1 {
  const marker = ":resume:";
  const baseId = original.assignmentId.includes(marker)
    ? original.assignmentId.slice(0, original.assignmentId.indexOf(marker))
    : original.assignmentId;
  const suffix = `${marker}${attempt}`;
  const retryInstruction = `Resume failed attempt ${attempt} in the existing verified workspace. Resolve the prior failure without repeating completed role work. Resolve contextReferences relative to the current workspace root; never use a parent or sibling worktree.`;
  const objectiveMarker = "Original assignment objective: ";
  const markerIndex = original.objective.lastIndexOf(objectiveMarker);
  const originalObjective = (
    markerIndex >= 0
      ? original.objective.slice(markerIndex + objectiveMarker.length)
      : original.objective
  ).slice(0, 1_000);
  const validationFeedback = failureFeedback
    ? ` Conduit rejected the previous turn for this exact reason: ${failureFeedback.slice(0, 1_000)}. Correct that failure and return a new complete AgentResponseV1.`
    : "";
  const assignment = createAgentAssignmentV1({
    assignmentId: `${baseId.slice(0, 120 - suffix.length)}${suffix}`,
    role: original.role,
    roleKind: original.roleKind,
    objective:
      `${retryInstruction}${validationFeedback} ${objectiveMarker}${originalObjective}`.slice(
        0,
        2_400,
      ),
    ownedPaths: original.ownedPaths,
    forbiddenPaths: original.forbiddenPaths,
    dependencies: original.dependencies,
    contextReferences: original.contextReferences,
    acceptanceCriteria: original.acceptanceCriteria,
    contracts: original.contracts,
    requiredVerification: original.requiredVerification,
    expectedCapabilities: original.expectedCapabilities,
  });
  const validation = validateAgentAssignmentV1(assignment);
  if (!validation.valid)
    throw new Error(
      `Invalid resumed assignment: ${validation.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
    );
  return assignment;
}

async function featurePackageRoot(
  projectRoot: string,
  specsDirectory: string,
  featureId: string,
): Promise<string | undefined> {
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
  return featureDirectory
    ? path.join(specsRoot, featureDirectory.name)
    : undefined;
}

async function featurePacketSnapshot(
  projectRoot: string,
  specsDirectory: string,
  featureId: string,
): Promise<string> {
  const root = await featurePackageRoot(projectRoot, specsDirectory, featureId);
  if (!root)
    return "The approved feature packet was not found when this assignment was planned.";
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
  sharedReadOnlyWorkspace = false,
}: {
  projectRoot: string;
  config: Config;
  featureId: string;
  roleNames: string[];
  builtinRoot: string;
  fetchSkills?: boolean;
  sharedReadOnlyWorkspace?: boolean;
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
  if (!sharedReadOnlyWorkspace && projectStatus(projectRoot, stateDirectory))
    throw new Error(
      "Agent runs require a clean project worktree; commit, stash, or discard material project changes before starting.",
    );
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
  const packageRoot = await featurePackageRoot(
    projectRoot,
    config.specsDir,
    featureId,
  );
  if (!packageRoot)
    throw new Error(`Approved feature package ${featureId} was not found.`);
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
  assertDistinctRoleWorkspaceKeys(roles.map((role) => role.name));
  const packageIdentity = await hashFeaturePackage({
    packageRoot,
    ownershipInputs: roles.map((role) => ({
      role: role.name,
      readOnly: role.readOnly,
      owns: role.owns,
      dependsOn: role.dependsOn,
    })),
  });
  const repositoryIdentity = resolveRepositoryIdentity(projectRoot);
  const startingHead = repositoryHead(projectRoot);
  for (const role of roles) {
    if (sharedReadOnlyWorkspace) {
      role.readOnly = true;
      role.worktree = projectRoot;
      continue;
    }
    const roleKey = normalizeRoleWorkspaceKey(role.name);
    role.workspaceRepositoryId = repositoryIdentity.repositoryId;
    role.workspaceRoleKey = roleKey;
    role.workspaceBranchName = `conduit/${runId}/${roleKey}`;
    role.workspaceAssignmentHash = assignmentIdentityHash(role.assignment!);
    role.workspaceLeaseOwner = `${runId}:${roleKey}`;
    role.worktree = roleWorkspaceSlotPath(
      configuredWorktreeRoot,
      repositoryIdentity.repositoryId,
      roleKey,
    );
  }
  const run: Run = {
    id: runId,
    featureId,
    status: "planned",
    createdAt: new Date().toISOString(),
    roles,
    startingHead,
    featurePackageHash: packageIdentity.hash,
    featurePackagePath: path.relative(projectRoot, packageRoot),
    reviewerWorkflow: { correctionRound: 0, findingFingerprints: [] },
    stateDirectory,
    worktreeRoot: configuredWorktreeRoot,
    worktreeRetentionDays: config.worktreeRetentionDays ?? 7,
    runDiagnosticsRetentionDays: config.runDiagnosticsRetentionDays ?? 30,
  };
  return { run, runDir };
}

function worktreePath(projectRoot: string, run: Run, role: RunRole): string {
  if (role.worktree) return path.resolve(role.worktree);
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
    if (record.status !== "completed") continue;
    const hasRegisteredLegacyWorktree = record.worktrees.some((worktree) =>
      gitWorktreeRegistry.find(projectRoot, worktree),
    );
    if (!hasRegisteredLegacyWorktree) await repository.remove(record.runId);
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

function materializeDependencyTrees(
  projectRoot: string,
  worktree: string,
  excludedDirectories: readonly string[],
): string[] {
  const materialized: string[] = [];
  for (const source of dependencyTreePaths(projectRoot, excludedDirectories)) {
    const relativePath = path.relative(projectRoot, source);
    const destination = path.join(worktree, relativePath);
    if (existsSync(destination)) continue;
    mkdirSync(path.dirname(destination), { recursive: true });
    cpSync(source, destination, {
      recursive: true,
      force: false,
      errorOnExist: true,
      verbatimSymlinks: true,
      mode: fsConstants.COPYFILE_FICLONE,
    });
    materialized.push(relativePath);
  }
  return materialized;
}

function materializeRoleDependencyTrees(
  run: Run,
  role: RunRole,
  worktree: string,
): void {
  const inherited = role.dependsOn.flatMap((dependency) => {
    const source = run.roles.find(
      (candidate) => candidate.name === dependency,
    )?.worktree;
    return source
      ? materializeDependencyTrees(source, worktree, [worktree])
      : [];
  });
  role.linkedWorkspacePaths = [
    ...new Set([...(role.linkedWorkspacePaths ?? []), ...inherited]),
  ];
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

function restoreDisposableReviewerWorktree(
  worktree: string,
  baselineHead: string,
): void {
  const reset = spawnSync(
    "git",
    ["-C", worktree, "reset", "--hard", baselineHead],
    {
      encoding: "utf8",
    },
  );
  if (reset.status !== 0)
    throw new Error("Could not restore the disposable reviewer workspace.");
  const clean = spawnSync("git", ["-C", worktree, "clean", "-ffdx"], {
    encoding: "utf8",
  });
  if (clean.status !== 0)
    throw new Error("Could not clean the disposable reviewer workspace.");
}

function observedProvisioningHead(
  projectRoot: string,
  role: RunRole,
): string | undefined {
  if (!role.worktree)
    throw new Error(`Role ${role.name} has no registered workspace path.`);
  const expected = path.resolve(role.worktree);
  if (!existsSync(expected)) {
    if (gitWorktreeRegistry.find(projectRoot, expected))
      throw new Error(
        `Role ${role.name} workspace path is still registered by Git.`,
      );
    if (role.workspaceBranchName) {
      const branch = spawnSync(
        "git",
        [
          "-C",
          projectRoot,
          "rev-parse",
          "--verify",
          `refs/heads/${role.workspaceBranchName}`,
        ],
        { encoding: "utf8" },
      );
      if (branch.status === 0)
        throw new Error(
          `Role ${role.name} branch exists without its registered worktree.`,
        );
    }
    return undefined;
  }
  const root = spawnSync(
    "git",
    ["-C", expected, "rev-parse", "--show-toplevel"],
    {
      encoding: "utf8",
    },
  );
  if (root.status !== 0 || path.resolve(root.stdout.trim()) !== expected)
    throw new Error(
      `Role ${role.name} provisioning path is not the registered Git worktree.`,
    );
  if (
    role.workspaceRepositoryId &&
    resolveRepositoryIdentity(expected).repositoryId !==
      role.workspaceRepositoryId
  )
    throw new Error(
      `Role ${role.name} provisioning repository identity diverged.`,
    );
  const branch = spawnSync(
    "git",
    ["-C", expected, "symbolic-ref", "--quiet", "--short", "HEAD"],
    { encoding: "utf8" },
  );
  if (
    !role.workspaceBranchName ||
    branch.status !== 0 ||
    branch.stdout.trim() !== role.workspaceBranchName
  )
    throw new Error(`Role ${role.name} provisioning branch identity diverged.`);
  const status = spawnSync(
    "git",
    ["-C", expected, "status", "--porcelain", "--untracked-files=all"],
    { encoding: "utf8" },
  );
  if (status.status !== 0 || status.stdout.trim())
    throw new Error(`Role ${role.name} provisioning workspace is not clean.`);
  const head = repositoryHead(expected);
  if (!head)
    throw new Error(`Role ${role.name} provisioning HEAD is unavailable.`);
  return head;
}

function verifyReusableWorktree(
  role: RunRole,
  allowFailedTurnChanges = false,
): string {
  if (!role.worktree || !role.worktreeHead)
    throw new Error(
      `Role ${role.name} has no verified reusable worktree baseline.`,
    );
  const expected = path.resolve(role.worktree);
  const root = spawnSync(
    "git",
    ["-C", expected, "rev-parse", "--show-toplevel"],
    { encoding: "utf8" },
  );
  if (root.status !== 0 || path.resolve(root.stdout.trim()) !== expected)
    throw new Error(`Role ${role.name} worktree identity is no longer valid.`);
  if (
    role.workspaceRepositoryId &&
    resolveRepositoryIdentity(expected).repositoryId !==
      role.workspaceRepositoryId
  )
    throw new Error(`Role ${role.name} repository identity has changed.`);
  if (role.workspaceBranchName) {
    const branch = spawnSync(
      "git",
      ["-C", expected, "symbolic-ref", "--quiet", "--short", "HEAD"],
      { encoding: "utf8" },
    );
    if (
      branch.status !== 0 ||
      branch.stdout.trim() !== role.workspaceBranchName
    )
      throw new Error(
        `Role ${role.name} workspace branch identity has changed.`,
      );
  }
  const head = spawnSync("git", ["-C", expected, "rev-parse", "HEAD"], {
    encoding: "utf8",
  });
  if (head.status !== 0 || head.stdout.trim() !== role.worktreeHead)
    throw new Error(`Role ${role.name} worktree baseline has diverged.`);
  const status = spawnSync(
    "git",
    ["-C", expected, "status", "--porcelain", "--untracked-files=all"],
    { encoding: "utf8" },
  );
  if (status.status !== 0)
    throw new Error(`Role ${role.name} worktree status is unavailable.`);
  const recordedLinks = new Set(
    (role.linkedWorkspacePaths ?? []).map((relativePath) => {
      const target = path.resolve(expected, relativePath);
      const relative = path.relative(expected, target);
      if (
        relative === ".." ||
        relative.startsWith(`..${path.sep}`) ||
        path.isAbsolute(relative)
      )
        throw new Error(`Recorded workspace link escaped ${role.name}.`);
      let stat;
      try {
        stat = lstatSync(target);
      } catch {
        return relative.replaceAll(path.sep, "/");
      }
      if (!stat.isSymbolicLink() && !stat.isDirectory())
        throw new Error(
          `Recorded workspace dependency ${relativePath} has an invalid type.`,
        );
      return relative.replaceAll(path.sep, "/");
    }),
  );
  const disallowed = status.stdout
    .split("\n")
    .filter(Boolean)
    .map((line) => line.slice(3).replace(/\/$/, ""))
    .filter(
      (entry) =>
        ![...recordedLinks].some(
          (link) => entry === link || entry.startsWith(`${link}/`),
        ),
    );
  if (disallowed.length && !allowFailedTurnChanges)
    throw new Error(
      `Role ${role.name} worktree contains unexpected changes or generated artifacts: ${disallowed.join(", ")}`,
    );
  return expected;
}

function addWorktree(projectRoot: string, run: Run, role: RunRole): string {
  if (!repositoryHasHead(projectRoot))
    throw new Error(
      "Agent isolation requires a committed Git HEAD before a run can start.",
    );
  const target = worktreePath(projectRoot, run, role);
  const branch = role.workspaceBranchName ?? `conduit/${run.id}/${role.name}`;
  const disabledHooksDirectory = path.join(
    run.stateDirectory ?? path.join(projectRoot, ".conduit"),
    "hooks",
    "disabled",
  );
  mkdirSync(disabledHooksDirectory, { recursive: true });
  const diagnostic = gitWorktreeRegistry.add(
    projectRoot,
    target,
    branch,
    "HEAD",
    disabledHooksDirectory,
  );
  if (diagnostic)
    throw new Error(
      `Could not create worktree for ${role.name}: ${diagnostic}`,
    );
  try {
    concealTrackedAgentState(target);
    rmSync(path.join(target, ".conduit"), { recursive: true, force: true });
    rmSync(path.join(target, "state.db"), { force: true });
    role.linkedWorkspacePaths = materializeDependencyTrees(
      projectRoot,
      target,
      [target, run.stateDirectory, run.worktreeRoot].filter(
        (directory): directory is string => Boolean(directory),
      ),
    );
  } catch (cause) {
    gitWorktreeRegistry.remove(projectRoot, target);
    throw new Error(
      `Could not provision dependencies for ${role.name}: ${cause instanceof Error ? cause.message : String(cause)}`,
      { cause },
    );
  }
  return target;
}

function commitContainsIntegrableChanges(
  worktree: string,
  commit: string,
): boolean {
  const changed = spawnSync(
    "git",
    [
      "-C",
      worktree,
      "diff-tree",
      "--no-commit-id",
      "--name-only",
      "-r",
      commit,
    ],
    { encoding: "utf8" },
  );
  if (changed.status !== 0)
    throw new Error(`Could not inspect dependency commit ${commit}.`);
  const files = changed.stdout
    .split("\n")
    .map((file) => file.trim())
    .filter(Boolean);
  return files.some((file) => !isDependencyTreePath(file));
}

function integrateDependencyCommits(
  worktree: string,
  commits: readonly string[],
  disabledHooksDirectory: string,
): void {
  for (const commit of [...new Set(commits)]) {
    if (!commitContainsIntegrableChanges(worktree, commit)) continue;
    const result = spawnSync(
      "git",
      [
        "-c",
        `core.hooksPath=${disabledHooksDirectory}`,
        "-c",
        "commit.gpgSign=false",
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
  const parentTree = spawnSync(
    "git",
    ["-C", worktree, "rev-parse", `${parent.stdout.trim()}^{tree}`],
    { encoding: "utf8" },
  );
  if (parentTree.status !== 0)
    throw new Error(`Could not resolve ${roleName} artifact parent tree.`);
  if (tree.stdout.trim() === parentTree.stdout.trim()) return undefined;
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

function checkpointFailedTurnWorkspace(
  role: RunRole,
  disabledHooksDirectory: string,
): void {
  const worktree = verifyReusableWorktree(role, true);
  const files = changedFiles(worktree).filter(
    (file) => !isDependencyTreePath(file),
  );
  const commit = role.readOnly
    ? undefined
    : commitRoleArtifacts(worktree, role.name, files, disabledHooksDirectory);
  if (commit) {
    const reset = spawnSync(
      "git",
      ["-C", worktree, "reset", "--hard", commit],
      {
        encoding: "utf8",
      },
    );
    if (reset.status !== 0)
      throw new Error(`Could not restore ${role.name} failed-turn checkpoint.`);
    role.pendingResumeCommits = [...(role.pendingResumeCommits ?? []), commit];
    role.resumeObservedFiles = [
      ...new Set([...(role.resumeObservedFiles ?? []), ...files]),
    ];
  } else if (role.readOnly) {
    const reset = spawnSync(
      "git",
      ["-C", worktree, "reset", "--hard", role.worktreeHead!],
      { encoding: "utf8" },
    );
    if (reset.status !== 0)
      throw new Error(`Could not restore ${role.name} disposable workspace.`);
  }
  const clean = spawnSync("git", ["-C", worktree, "clean", "-ffdx"], {
    encoding: "utf8",
  });
  if (clean.status !== 0)
    throw new Error(`Could not clean ${role.name} failed-turn workspace.`);
  role.worktreeHead = repositoryHead(worktree);
  if (!role.worktreeHead)
    throw new Error(`Could not record ${role.name} failed-turn checkpoint.`);
}

function repositoryHead(projectRoot: string): string | undefined {
  const result = spawnSync(
    "git",
    ["-C", projectRoot, "rev-parse", "--verify", "HEAD"],
    { encoding: "utf8" },
  );
  return result.status === 0 ? result.stdout.trim() : undefined;
}

function repositoryHasHead(projectRoot: string): boolean {
  return Boolean(repositoryHead(projectRoot));
}

function projectStatus(projectRoot: string, stateDirectory?: string): string {
  const result = spawnSync(
    "git",
    ["-C", projectRoot, "status", "--porcelain", "--untracked-files=all"],
    { encoding: "utf8" },
  );
  if (result.status !== 0)
    throw new Error("Could not verify the project worktree status.");
  const relativeState = stateDirectory
    ? path.relative(projectRoot, stateDirectory).replaceAll(path.sep, "/")
    : undefined;
  return result.stdout
    .split("\n")
    .filter(Boolean)
    .filter((line) => {
      if (
        !relativeState ||
        relativeState === "." ||
        relativeState.startsWith("..")
      )
        return true;
      const entryPath = line.slice(3).replace(/\/$/, "");
      return (
        entryPath !== relativeState &&
        !entryPath.startsWith(`${relativeState}/`)
      );
    })
    .join("\n");
}

async function anchorRoleLineageInReviewer(input: {
  projectRoot: string;
  run: Run;
  reviewer: RunRole;
  repository?: RoleWorkspaceRepository;
}): Promise<void> {
  if (!input.reviewer.worktree || !input.reviewer.worktreeHead) return;
  const additionalParents = input.run.roles
    .filter((role) => role !== input.reviewer && role.worktreeHead)
    .map((role) => role.worktreeHead!)
    .filter(
      (head) =>
        spawnSync(
          "git",
          [
            "-C",
            input.projectRoot,
            "merge-base",
            "--is-ancestor",
            head,
            input.reviewer.worktreeHead!,
          ],
          { encoding: "utf8" },
        ).status !== 0,
    );
  if (!additionalParents.length) return;
  verifyReusableWorktree(input.reviewer);
  const tree = spawnSync(
    "git",
    [
      "-C",
      input.reviewer.worktree,
      "rev-parse",
      `${input.reviewer.worktreeHead}^{tree}`,
    ],
    { encoding: "utf8" },
  );
  if (tree.status !== 0)
    throw new Error("Could not resolve reviewed tree for lineage anchoring.");
  const committed = spawnSync(
    "git",
    [
      "-c",
      "user.name=Conduit",
      "-c",
      "user.email=conduit@localhost",
      "-C",
      input.reviewer.worktree,
      "commit-tree",
      tree.stdout.trim(),
      "-p",
      input.reviewer.worktreeHead,
      ...additionalParents.flatMap((head) => ["-p", head]),
      "-m",
      "conduit: anchor approved role lineage",
    ],
    { encoding: "utf8" },
  );
  if (committed.status !== 0)
    throw new Error("Could not anchor approved role lineage.");
  const anchoredHead = committed.stdout.trim();
  const reset = spawnSync(
    "git",
    ["-C", input.reviewer.worktree, "reset", "--hard", anchoredHead],
    { encoding: "utf8" },
  );
  if (reset.status !== 0)
    throw new Error("Could not advance reviewer lineage anchor.");
  input.reviewer.worktreeHead = anchoredHead;
  const identity = roleWorkspaceLeaseIdentity(input.run.id, input.reviewer);
  if (
    input.repository &&
    identity &&
    !(await input.repository.recordHead(identity, anchoredHead))
  )
    throw new Error("Could not persist reviewer lineage anchor.");
}

function promoteApprovedReviewerWorktree(input: {
  projectRoot: string;
  reviewer: RunRole;
  expectedProjectHead: string;
  disabledHooksDirectory: string;
  stateDirectory?: string;
}): void {
  if (repositoryHead(input.projectRoot) !== input.expectedProjectHead)
    throw new Error(
      "Project HEAD changed during the run; approved artifacts remain in the reviewer worktree.",
    );
  const status = projectStatus(input.projectRoot, input.stateDirectory);
  if (status)
    throw new Error(
      "Project worktree is not clean; preserve or commit local changes before integrating the approved reviewer worktree.",
    );
  const reviewerWorktree = verifyReusableWorktree(input.reviewer);
  const reviewerHead = repositoryHead(reviewerWorktree);
  if (!reviewerHead || reviewerHead !== input.reviewer.worktreeHead)
    throw new Error("Approved reviewer worktree baseline is unavailable.");
  const descendant = spawnSync(
    "git",
    [
      "-C",
      input.projectRoot,
      "merge-base",
      "--is-ancestor",
      input.expectedProjectHead,
      reviewerHead,
    ],
    { encoding: "utf8" },
  );
  if (descendant.status !== 0)
    throw new Error(
      "Approved reviewer worktree is not based on the project HEAD.",
    );
  const promoted = spawnSync(
    "git",
    [
      "-c",
      `core.hooksPath=${input.disabledHooksDirectory}`,
      "-c",
      "commit.gpgSign=false",
      "-C",
      input.projectRoot,
      "merge",
      "--ff-only",
      reviewerHead,
    ],
    { encoding: "utf8" },
  );
  if (promoted.status !== 0)
    throw new Error(
      `Could not fast-forward the project to the approved reviewer worktree: ${promoted.stderr.trim()}`,
    );
}

function roleWorkspaceLeaseIdentity(
  runId: string,
  role: RunRole,
): RoleWorkspaceLeaseIdentity | undefined {
  if (
    !role.workspaceRepositoryId ||
    !role.workspaceRoleKey ||
    !role.workspaceLeaseOwner ||
    role.workspaceFencingToken === undefined
  )
    return undefined;
  return {
    repositoryId: role.workspaceRepositoryId,
    roleKey: role.workspaceRoleKey,
    owningRunId: runId,
    leaseOwner: role.workspaceLeaseOwner,
    fencingToken: role.workspaceFencingToken,
  };
}

function removeRecordedWorkspaceLinks(role: RunRole): void {
  if (!role.worktree) return;
  const root = path.resolve(role.worktree);
  for (const relativePath of role.linkedWorkspacePaths ?? []) {
    const target = path.resolve(root, relativePath);
    const relative = path.relative(root, target);
    if (
      relative === ".." ||
      relative.startsWith(`..${path.sep}`) ||
      path.isAbsolute(relative)
    )
      throw new Error(`Recorded workspace link escaped ${role.name}.`);
    let stat;
    try {
      stat = lstatSync(target);
    } catch {
      continue;
    }
    if (!stat.isSymbolicLink() && !stat.isDirectory())
      throw new Error(
        `Recorded workspace dependency ${relativePath} has an invalid type.`,
      );
    rmSync(target, { recursive: true, force: true });
  }
}

async function cleanupPromotedRoleWorkspace(input: {
  projectRoot: string;
  runId: string;
  role: RunRole;
  promotedHead: string;
  repository: RoleWorkspaceRepository;
}): Promise<void> {
  const identity = roleWorkspaceLeaseIdentity(input.runId, input.role);
  if (!identity || !input.role.worktree || !input.role.worktreeHead)
    throw new Error(
      `Role ${input.role.name} has no fenced workspace identity.`,
    );
  const slot = await input.repository.load(
    identity.repositoryId,
    identity.roleKey,
  );
  if (
    !slot ||
    path.resolve(slot.workspacePath) !== path.resolve(input.role.worktree)
  )
    throw new Error(`Role ${input.role.name} workspace registry diverged.`);
  if (
    slot.branchName !== input.role.workspaceBranchName ||
    (slot.worktreeHead !== undefined &&
      slot.worktreeHead !== input.role.worktreeHead)
  )
    throw new Error(`Role ${input.role.name} workspace Git identity diverged.`);
  const branchOid = repositoryHead(input.role.worktree);
  if (!branchOid || branchOid !== input.role.worktreeHead)
    throw new Error(
      `Role ${input.role.name} branch OID changed before cleanup.`,
    );
  const ancestor = spawnSync(
    "git",
    [
      "-C",
      input.projectRoot,
      "merge-base",
      "--is-ancestor",
      branchOid,
      input.promotedHead,
    ],
    { encoding: "utf8" },
  );
  if (ancestor.status !== 0)
    throw new Error(
      `Role ${input.role.name} branch is not contained in the promoted HEAD.`,
    );
  if (
    !(await input.repository.completeGeneration(identity, {
      branchOid,
      outcome: "promoted",
      promotionOid: input.promotedHead,
    }))
  )
    throw new Error(
      `Role ${input.role.name} generation lineage could not be completed.`,
    );
  if (
    !(await input.repository.transition(
      identity,
      slot.state,
      RoleWorkspaceState.CleanupPending,
    ))
  )
    throw new Error(
      `Role ${input.role.name} cleanup state could not be persisted.`,
    );
  verifyReusableWorktree(input.role);
  removeRecordedWorkspaceLinks(input.role);
  const status = spawnSync(
    "git",
    [
      "-C",
      input.role.worktree,
      "status",
      "--porcelain",
      "--untracked-files=all",
      "--ignored=matching",
    ],
    { encoding: "utf8" },
  );
  if (status.status !== 0 || status.stdout.trim())
    throw new Error(
      `Role ${input.role.name} workspace is not clean enough to remove.`,
    );
  const removed = gitWorktreeRegistry.remove(
    input.projectRoot,
    input.role.worktree,
  );
  if (!removed)
    throw new Error(`Role ${input.role.name} worktree removal failed.`);
  const deleted = spawnSync(
    "git",
    [
      "-C",
      input.projectRoot,
      "update-ref",
      "-d",
      `refs/heads/${slot.branchName}`,
      branchOid,
    ],
    { encoding: "utf8" },
  );
  if (deleted.status !== 0)
    throw new Error(`Role ${input.role.name} branch changed during cleanup.`);
  if (
    !(await input.repository.remove(
      identity,
      RoleWorkspaceState.CleanupPending,
    ))
  )
    throw new Error(
      `Role ${input.role.name} slot cleanup could not be finalized.`,
    );
}

async function reconcileCleanupPendingRoleWorkspaces(input: {
  projectRoot: string;
  repositoryId: string;
  repository: RoleWorkspaceRepository;
}): Promise<void> {
  if (
    resolveRepositoryIdentity(input.projectRoot).repositoryId !==
    input.repositoryId
  )
    throw new Error("Cleanup-pending workspace repository identity diverged.");
  for (const slot of await input.repository.listCleanupCandidates(
    input.repositoryId,
  )) {
    const generations = await input.repository.listGenerations(
      slot.repositoryId,
      slot.roleKey,
    );
    const generation = generations.find(
      (candidate) => candidate.generation === slot.generation,
    );
    if (
      !generation?.branchOid ||
      generation.outcome !== "promoted" ||
      !generation.promotionOid ||
      !generation.completedAt ||
      generation.branchName !== slot.branchName ||
      path.resolve(generation.workspacePath) !==
        path.resolve(slot.workspacePath)
    )
      continue;
    if (
      spawnSync(
        "git",
        [
          "-C",
          input.projectRoot,
          "cat-file",
          "-e",
          `${generation.branchOid}^{commit}`,
        ],
        { encoding: "utf8" },
      ).status !== 0 ||
      spawnSync(
        "git",
        [
          "-C",
          input.projectRoot,
          "cat-file",
          "-e",
          `${generation.promotionOid}^{commit}`,
        ],
        { encoding: "utf8" },
      ).status !== 0 ||
      spawnSync(
        "git",
        [
          "-C",
          input.projectRoot,
          "merge-base",
          "--is-ancestor",
          generation.branchOid,
          generation.promotionOid,
        ],
        { encoding: "utf8" },
      ).status !== 0
    )
      continue;
    const identity: RoleWorkspaceLeaseIdentity = {
      repositoryId: slot.repositoryId,
      roleKey: slot.roleKey,
      owningRunId: slot.owningRunId,
      leaseOwner: slot.leaseOwner ?? "",
      fencingToken: slot.fencingToken,
    };
    if (!slot.leaseOwner) continue;
    if (slot.state !== RoleWorkspaceState.CleanupPending) {
      if (
        !(await input.repository.transition(
          identity,
          slot.state,
          RoleWorkspaceState.CleanupPending,
        ))
      )
        continue;
    }
    if (existsSync(slot.workspacePath)) {
      const root = spawnSync(
        "git",
        ["-C", slot.workspacePath, "rev-parse", "--show-toplevel"],
        { encoding: "utf8" },
      );
      const branch = spawnSync(
        "git",
        [
          "-C",
          slot.workspacePath,
          "symbolic-ref",
          "--quiet",
          "--short",
          "HEAD",
        ],
        { encoding: "utf8" },
      );
      if (
        root.status !== 0 ||
        path.resolve(root.stdout.trim()) !== path.resolve(slot.workspacePath) ||
        resolveRepositoryIdentity(slot.workspacePath).repositoryId !==
          slot.repositoryId ||
        branch.status !== 0 ||
        branch.stdout.trim() !== slot.branchName ||
        repositoryHead(slot.workspacePath) !== generation.branchOid
      )
        continue;
      const cleaned = spawnSync(
        "git",
        ["-C", slot.workspacePath, "clean", "-ffdx"],
        { encoding: "utf8" },
      );
      const status = spawnSync(
        "git",
        [
          "-C",
          slot.workspacePath,
          "status",
          "--porcelain",
          "--untracked-files=all",
          "--ignored=matching",
        ],
        { encoding: "utf8" },
      );
      if (cleaned.status !== 0 || status.status !== 0 || status.stdout.trim())
        continue;
      if (!gitWorktreeRegistry.remove(input.projectRoot, slot.workspacePath))
        continue;
    } else if (
      gitWorktreeRegistry.find(input.projectRoot, slot.workspacePath)
    ) {
      if (!gitWorktreeRegistry.remove(input.projectRoot, slot.workspacePath))
        continue;
    }
    const branch = spawnSync(
      "git",
      [
        "-C",
        input.projectRoot,
        "rev-parse",
        "--verify",
        `refs/heads/${slot.branchName}`,
      ],
      { encoding: "utf8" },
    );
    if (branch.status === 0) {
      if (
        branch.stdout.trim() !== generation.branchOid ||
        spawnSync(
          "git",
          [
            "-C",
            input.projectRoot,
            "merge-base",
            "--is-ancestor",
            generation.branchOid,
            generation.promotionOid,
          ],
          { encoding: "utf8" },
        ).status !== 0
      )
        continue;
      const deleted = spawnSync(
        "git",
        [
          "-C",
          input.projectRoot,
          "update-ref",
          "-d",
          `refs/heads/${slot.branchName}`,
          generation.branchOid,
        ],
        { encoding: "utf8" },
      );
      if (deleted.status !== 0) continue;
    }
    if (
      !(await input.repository.completeGeneration(identity, {
        branchOid: generation.branchOid,
        outcome: generation.outcome,
        promotionOid: generation.promotionOid,
      }))
    )
      continue;
    await input.repository.remove(identity, RoleWorkspaceState.CleanupPending);
  }
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

function terminalRunStatus(
  results: readonly RunResult[],
  reviewGateApproved: boolean,
): TerminalRunStatus {
  if (results.some((result) => result.status === "cancelled")) {
    return "cancelled";
  }
  if (
    reviewGateApproved &&
    results.every((result) => result.status === "completed")
  ) {
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
    ["-C", cwd, "ls-files", "--others", "--exclude-standard"],
    { encoding: "utf8" },
  );
  const files = [
    ...(tracked.status === 0 ? tracked.stdout.split("\n") : []),
    ...(untracked.status === 0 ? untracked.stdout.split("\n") : []),
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
  const dependencyPaths = (role.linkedWorkspacePaths ?? []).map((entry) =>
    entry.replaceAll("\\", "/").replace(/\/$/, ""),
  );
  const files = [
    ...new Set([
      ...(role.resumeObservedFiles ?? []),
      ...filesChangedSince(cwd, observedChangedFiles),
    ]),
  ].filter(
    (file) =>
      !dependencyPaths.some(
        (dependency) =>
          file === dependency || file.startsWith(`${dependency}/`),
      ),
  );
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
  let protocolFailureMessage: string | undefined;
  let failureKind: RunResult["failureKind"];
  if (!protocolCompleted && !runnerFailureMessage) {
    const message = !structural.valid
      ? `The agent did not return a valid AgentResponseV1 object: ${structural.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}.`
      : structural.value?.status !== "completed"
        ? `The agent returned ${structural.value?.status}: ${structural.value?.summary}`
        : `The agent response violated its assignment policy: ${semantic.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}.`;
    protocolFailureMessage = message.slice(0, 2_000);
    failureKind = !finalResponseRaw
      ? "missing-response"
      : !structural.valid
        ? "structural-response"
        : structural.value?.status !== "completed"
          ? "reported-noncompletion"
          : "semantic-response";
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
        message: protocolFailureMessage,
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
    error: protocolFailureMessage ?? runnerFailureMessage,
    retryable:
      failureKind === "missing-response" ||
      failureKind === "structural-response" ||
      failureKind === "semantic-response",
    failureKind: failureKind ?? (runnerFailureMessage ? "runner" : undefined),
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
  resume = false,
  maxAutomaticRetries = maxAutomaticRoleRetries,
  onProgress = () => {},
  onChange = () => {},
  onRoleWorkspaceReady,
  signal,
  eventRepository,
  processRegistry,
  resultRepository,
  runtimeEventRepository,
  communicationProviders,
  roleWorkspaceRepository,
  sharedReadOnlyWorkspace = false,
}: {
  projectRoot: string;
  run: Run;
  runDir: string;
  dryRun?: boolean;
  resume?: boolean;
  maxAutomaticRetries?: number;
  onProgress?: (message: string) => void;
  onChange?: (event: { summary: string; preview: string }) => void;
  onRoleWorkspaceReady?: () => Promise<void>;
  signal?: AbortSignal;
  eventRepository?: RunEventRepository;
  processRegistry?: RunProcessRegistry;
  resultRepository?: ConduitResultRecordRepository;
  runtimeEventRepository?: import("../interfaces/runtime-event-repository.js").RuntimeEventRepository;
  communicationProviders?: readonly import("@system/communication/types/provider.js").AgentCommunicationProvider[];
  roleWorkspaceRepository?: RoleWorkspaceRepository;
  sharedReadOnlyWorkspace?: boolean;
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
  const rolesRequiringWorkspace = run.roles;
  if (
    !sharedReadOnlyWorkspace &&
    roleWorkspaceRepository &&
    (!run.startingHead ||
      !run.featurePackageHash ||
      rolesRequiringWorkspace.some(
        (role) =>
          !role.workspaceRepositoryId ||
          !role.workspaceRoleKey ||
          !role.workspaceBranchName ||
          !role.workspaceAssignmentHash ||
          !role.workspaceLeaseOwner ||
          !role.worktree,
      ))
  )
    throw new Error(
      "Managed run contains a role without canonical workspace metadata.",
    );
  if (roleWorkspaceRepository) {
    for (const repositoryId of [
      ...new Set(
        rolesRequiringWorkspace
          .map((role) => role.workspaceRepositoryId)
          .filter((value): value is string => Boolean(value)),
      ),
    ])
      await reconcileCleanupPendingRoleWorkspaces({
        projectRoot,
        repositoryId,
        repository: roleWorkspaceRepository,
      });
  }
  if (roleWorkspaceRepository && resume) {
    for (const role of run.roles) {
      if (
        !role.workspaceRepositoryId ||
        !role.workspaceRoleKey ||
        !role.workspaceBranchName ||
        !role.workspaceAssignmentHash ||
        !role.workspaceLeaseOwner ||
        !role.worktree
      )
        throw new Error(
          `Role ${role.name} has no canonical workspace identity.`,
        );
      const slot = await roleWorkspaceRepository.load(
        role.workspaceRepositoryId,
        role.workspaceRoleKey,
      );
      if (!slot || slot.owningRunId !== run.id)
        throw new Error(`Role ${role.name} has no canonical workspace slot.`);
    }
  }
  const managedRoles = run.roles.filter(
    (role) =>
      role.workspaceRepositoryId &&
      role.workspaceRoleKey &&
      role.workspaceBranchName &&
      role.workspaceAssignmentHash &&
      role.workspaceLeaseOwner &&
      role.worktree &&
      run.startingHead &&
      run.featurePackageHash,
  );
  if (managedRoles.length && !roleWorkspaceRepository)
    throw new Error(
      "Role workspace registry is unavailable for this planned run.",
    );
  if (roleWorkspaceRepository) {
    for (const role of managedRoles) {
      const existing = await roleWorkspaceRepository.load(
        role.workspaceRepositoryId!,
        role.workspaceRoleKey!,
      );
      if (existing && existing.owningRunId !== run.id)
        throw new Error(
          `Role workspace ${role.workspaceRoleKey} is retained by run ${existing.owningRunId}.`,
        );
    }
    const claimed = await roleWorkspaceRepository.claimAll(
      managedRoles.map((role) => ({
        repositoryId: role.workspaceRepositoryId!,
        roleKey: role.workspaceRoleKey!,
        workspacePath: path.resolve(role.worktree!),
        owningRunId: run.id,
        startingHead: run.startingHead!,
        packageHash: run.featurePackageHash!,
        assignmentHash: role.workspaceAssignmentHash!,
        branchName: role.workspaceBranchName!,
        leaseOwner: role.workspaceLeaseOwner!,
      })),
    );
    if (claimed.status !== "claimed")
      throw new Error(
        `Role workspaces could not be claimed; one is owned by run ${claimed.owningRunId}.`,
      );
    for (const role of managedRoles) {
      const slot = claimed.slots.find(
        (candidate) => candidate.roleKey === role.workspaceRoleKey,
      );
      if (!slot)
        throw new Error(`Role workspace ${role.name} was not claimed.`);
      role.workspaceFencingToken = slot.fencingToken;
    }
    await onRoleWorkspaceReady?.();
    for (const role of managedRoles) {
      const slot = claimed.slots.find(
        (candidate) => candidate.roleKey === role.workspaceRoleKey,
      )!;
      if (
        slot.state === RoleWorkspaceState.Provisioning &&
        !slot.worktreeHead
      ) {
        const observedHead = observedProvisioningHead(projectRoot, role);
        if (observedHead) {
          const identity = roleWorkspaceLeaseIdentity(run.id, role);
          if (
            !identity ||
            !(await roleWorkspaceRepository.recordHead(identity, observedHead))
          )
            throw new Error(
              `Role ${role.name} provisioning HEAD could not be reconciled.`,
            );
          role.worktreeHead = observedHead;
        }
      }
    }
    await onRoleWorkspaceReady?.();
  }
  const reviewerRequested = run.roles.some((role) => role.name === "reviewer");
  const projectHeadAtStart = reviewerRequested
    ? (run.startingHead ?? repositoryHead(projectRoot))
    : undefined;
  if (reviewerRequested && !projectHeadAtStart)
    throw new Error("Reviewed integration requires a committed project HEAD.");
  const runDisabledHooksDirectory = path.join(
    run.stateDirectory ?? path.join(projectRoot, ".conduit"),
    "hooks",
    "disabled",
  );
  rmSync(runDisabledHooksDirectory, { recursive: true, force: true });
  mkdirSync(runDisabledHooksDirectory, { recursive: true });
  if (resume) {
    for (const role of managedRoles) {
      const recoverableFailedTurn =
        role.lastFailureKind === "missing-response" ||
        role.lastFailureKind === "structural-response" ||
        role.lastFailureKind === "semantic-response";
      if (!recoverableFailedTurn || !role.worktree || !role.worktreeHead)
        continue;
      const status = spawnSync(
        "git",
        ["-C", role.worktree, "status", "--porcelain", "--untracked-files=all"],
        { encoding: "utf8" },
      );
      if (status.status !== 0)
        throw new Error(`Role ${role.name} failed-turn status is unavailable.`);
      if (!status.stdout.trim()) continue;
      checkpointFailedTurnWorkspace(role, runDisabledHooksDirectory);
      const identity = roleWorkspaceLeaseIdentity(run.id, role);
      if (
        roleWorkspaceRepository &&
        (!identity ||
          !(await roleWorkspaceRepository.recordHead(
            identity,
            role.worktreeHead,
          )))
      )
        throw new Error(
          `Could not persist ${role.name} failed-turn checkpoint.`,
        );
      await onRoleWorkspaceReady?.();
    }
  }
  const launchRole = async (
    role: RunRole,
    options: {
      readonly reuseWorktree?: boolean;
      readonly additionalCommits?: readonly string[];
      readonly logSuffix?: string;
    } = {},
  ): Promise<RunResult> => {
    onProgress(
      `${role.name}: ${options.reuseWorktree ? "reusing" : "preparing"} isolated worktree`,
    );
    let cwd: string;
    if (sharedReadOnlyWorkspace) {
      cwd = role.worktree ? path.resolve(role.worktree) : projectRoot;
    } else if (options.reuseWorktree) {
      const target = worktreePath(projectRoot, run, role);
      materializeRoleDependencyTrees(run, role, target);
      cwd = verifyReusableWorktree(role);
    } else {
      cwd = addWorktree(projectRoot, run, role);
      materializeRoleDependencyTrees(run, role, cwd);
    }
    role.worktree = cwd;
    const disabledHooksDirectory = runDisabledHooksDirectory;
    const inheritedCommits = [
      ...new Set([
        ...role.dependsOn.flatMap(
          (dependency) =>
            run.roles.find((candidate) => candidate.name === dependency)
              ?.integrationCommits ?? [],
        ),
        ...(options.additionalCommits ?? []),
      ]),
    ];
    const integratedCommits = role.integrationCommits ?? [];
    const newCommits = inheritedCommits.filter(
      (commit) => !integratedCommits.includes(commit),
    );
    if (!sharedReadOnlyWorkspace)
      integrateDependencyCommits(cwd, newCommits, disabledHooksDirectory);
    role.integrationCommits = [...integratedCommits, ...newCommits];
    const preTurnHead = repositoryHead(cwd);
    if (!preTurnHead && !sharedReadOnlyWorkspace)
      throw new Error(`Could not record ${role.name} pre-turn baseline.`);
    if (preTurnHead) {
      role.diffBaselineHead ??= preTurnHead;
      role.worktreeHead = preTurnHead;
    }
    const preTurnIdentity = roleWorkspaceLeaseIdentity(run.id, role);
    if (
      roleWorkspaceRepository &&
      preTurnIdentity &&
      preTurnHead &&
      !(await roleWorkspaceRepository.recordHead(preTurnIdentity, preTurnHead))
    )
      throw new Error(
        `Could not persist ${role.name} pre-turn workspace HEAD.`,
      );
    if (
      roleWorkspaceRepository &&
      role.workspaceRepositoryId &&
      role.workspaceRoleKey &&
      role.workspaceLeaseOwner &&
      role.workspaceFencingToken !== undefined
    ) {
      const slot = await roleWorkspaceRepository.load(
        role.workspaceRepositoryId,
        role.workspaceRoleKey,
      );
      if (
        !slot ||
        (slot.state !== RoleWorkspaceState.Running &&
          !(await roleWorkspaceRepository.transition(
            {
              repositoryId: role.workspaceRepositoryId,
              roleKey: role.workspaceRoleKey,
              owningRunId: run.id,
              leaseOwner: role.workspaceLeaseOwner,
              fencingToken: role.workspaceFencingToken,
            },
            slot.state,
            RoleWorkspaceState.Running,
          )))
      )
        throw new Error(`Role workspace ${role.name} lost its fenced lease.`);
    }
    await onRoleWorkspaceReady?.();
    const result = await runProcess(
      role,
      run.id,
      cwd,
      path.join(
        runDir,
        `${role.name}${options.logSuffix ? `-${options.logSuffix}` : ""}.log`,
      ),
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
    if (role.readOnly && !sharedReadOnlyWorkspace && preTurnHead)
      restoreDisposableReviewerWorktree(cwd, preTurnHead);
    const committableFiles = (result.files ?? []).filter(
      (file) => !isDependencyTreePath(file),
    );
    const commit = role.readOnly
      ? undefined
      : commitRoleArtifacts(
          cwd,
          role.name,
          committableFiles,
          disabledHooksDirectory,
        );
    if (commit) {
      const advanced = spawnSync(
        "git",
        ["-C", cwd, "reset", "--hard", commit],
        { encoding: "utf8" },
      );
      if (advanced.status !== 0)
        throw new Error(`Could not advance ${role.name} correction baseline.`);
    }
    if (result.status === "completed") {
      role.integrationCommits = [
        ...(role.integrationCommits ?? []),
        ...(role.pendingResumeCommits ?? []),
        ...(commit ? [commit] : []),
      ];
      role.pendingResumeCommits = [];
      role.resumeObservedFiles = [];
    } else if (commit) {
      role.pendingResumeCommits = [
        ...(role.pendingResumeCommits ?? []),
        commit,
      ];
      role.resumeObservedFiles = [
        ...new Set([
          ...(role.resumeObservedFiles ?? []),
          ...(result.files ?? []),
        ]),
      ];
    }
    const head = spawnSync("git", ["-C", cwd, "rev-parse", "HEAD"], {
      encoding: "utf8",
    });
    if (head.status !== 0 && !sharedReadOnlyWorkspace)
      throw new Error(`Could not record ${role.name} worktree baseline.`);
    if (head.status === 0) role.worktreeHead = head.stdout.trim();
    const workspaceIdentity = roleWorkspaceLeaseIdentity(run.id, role);
    if (
      roleWorkspaceRepository &&
      workspaceIdentity &&
      role.worktreeHead &&
      !(await roleWorkspaceRepository.recordHead(
        workspaceIdentity,
        role.worktreeHead,
      ))
    )
      throw new Error(`Could not persist ${role.name} workspace HEAD.`);
    if (result.status !== "dry-run") role.status = result.status;
    role.lastFailureKind = result.failureKind;
    await onRoleWorkspaceReady?.();
    return result;
  };
  const launchRoleSafely = async (
    role: RunRole,
    options: Parameters<typeof launchRole>[1] = {},
  ): Promise<RunResult> => {
    try {
      return await launchRole(role, options);
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
  const automaticRetryLimit = Math.min(
    maxAutomaticRoleRetries,
    Math.max(0, maxAutomaticRetries),
  );
  const launchRoleWithAutomaticRetry = async (
    role: RunRole,
    options: Parameters<typeof launchRole>[1] = {},
  ): Promise<RunResult> => {
    const originalAssignment = role.assignment;
    let result = await launchRoleSafely(role, options);
    let retries = 0;
    while (
      result.status === "failed" &&
      result.retryable === true &&
      retries < automaticRetryLimit &&
      role.worktree &&
      role.worktreeHead &&
      originalAssignment &&
      !signal?.aborted
    ) {
      retries += 1;
      role.resumeAttempt = (role.resumeAttempt ?? 0) + 1;
      role.assignment = resumedAssignment(
        originalAssignment,
        role.resumeAttempt,
        result.error,
      );
      await eventRepository?.append({
        type: "activity",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId: run.id,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "activity",
          message: `${role.name}: automatically retrying failed turn ${retries}/${automaticRetryLimit} with Conduit validation feedback`,
        },
      });
      result = await launchRoleSafely(role, {
        reuseWorktree: true,
        logSuffix: `auto-retry-${retries}`,
      });
    }
    return result;
  };
  const results: RunResult[] = [];
  const terminalByRole = new Map<string, RunResult>();
  const resumedRoleNames = new Set<string>();
  const integrationCommitsAtResume = new Map<string, readonly string[]>();
  if (resume) {
    run.status = "running";
    for (const role of run.roles) {
      if (role.status === "completed") {
        const resultRecord = await effectiveResultRepository.load(
          run.id,
          role.name,
        );
        const preserved: RunResult = {
          role: role.name,
          status: "completed",
          files: resultRecord?.observedChangedFiles
            ? [...resultRecord.observedChangedFiles]
            : [],
          resultRecord,
        };
        results.push(preserved);
        terminalByRole.set(role.name, preserved);
        continue;
      }
      integrationCommitsAtResume.set(role.name, [
        ...(role.integrationCommits ?? []),
      ]);
      resumedRoleNames.add(role.name);
      role.status = "planned";
      if (role.worktree && role.assignment) {
        role.resumeAttempt = (role.resumeAttempt ?? 0) + 1;
        role.assignment = resumedAssignment(
          role.assignment,
          role.resumeAttempt,
        );
      }
    }
  }
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
    const launchRunnable = (role: RunRole) =>
      launchRoleWithAutomaticRetry(role, {
        reuseWorktree: resume && Boolean(role.worktreeHead),
        logSuffix:
          resume && role.resumeAttempt
            ? `resume-${role.resumeAttempt}`
            : undefined,
      });
    if (requiresSharedWorkspace) {
      for (const role of runnable)
        groupResults.push(await launchRunnable(role));
    } else {
      groupResults.push(...(await Promise.all(runnable.map(launchRunnable))));
    }
    for (const result of groupResults) {
      results.push(result);
      terminalByRole.set(result.role, result);
    }
  }
  const reviewerRole = run.roles.find((role) => role.name === "reviewer");
  const reviewerSelected = Boolean(reviewerRole);
  let reviewGateApproved = !reviewerRole;
  const emitReviewError = async (
    code: string,
    message: string,
    roleId = reviewerRole?.name ?? "reviewer",
  ): Promise<void> => {
    await eventRepository?.append({
      type: "error",
      provenance: RunnerEventProvenance.ConduitObserved,
      runId: run.id,
      roleId,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "error",
        code,
        message: redactSecrets(message),
        recoverable: false,
      },
    });
  };
  if (reviewerRole) {
    const baseAssignments = new Map(
      run.roles.flatMap((role) =>
        role.assignment ? ([[role.name, role.assignment]] as const) : [],
      ),
    );
    const reviewerWorkflow =
      run.reviewerWorkflow ??
      (run.reviewerWorkflow = { correctionRound: 0, findingFingerprints: [] });
    const seenReviews = new Set(reviewerWorkflow.findingFingerprints);
    let correctionRound = reviewerWorkflow.correctionRound;
    let reviewerResult = terminalByRole.get(reviewerRole.name);
    while (reviewerResult?.status === "completed") {
      const response = reviewerResult.resultRecord?.response;
      const decision = response?.verdict?.decision;
      if (decision === "approved") {
        reviewGateApproved = true;
        break;
      }
      if (
        !response ||
        (decision !== "needs_changes" && decision !== "rejected")
      ) {
        await emitReviewError(
          "REVIEW_VERDICT_NOT_APPROVED",
          `Reviewer completed without an actionable approval or correction verdict: ${decision ?? "missing"}.`,
        );
        break;
      }
      const fingerprint = reviewFingerprint(response);
      if (seenReviews.has(fingerprint)) {
        await emitReviewError(
          "REPEATED_REVIEW_FINDINGS",
          "Reviewer repeated an unchanged set of actionable findings.",
        );
        break;
      }
      if (correctionRound >= maxReviewerCorrectionRounds) {
        await emitReviewError(
          "REVIEW_CORRECTION_LIMIT_EXHAUSTED",
          `Reviewer still requires changes after ${maxReviewerCorrectionRounds} correction rounds.`,
        );
        break;
      }

      let routed: ReadonlyMap<RunRole, readonly AgentFindingV1[]>;
      try {
        routed = routeReviewFindings(response, run.roles, baseAssignments);
      } catch (cause) {
        await emitReviewError(
          "REVIEW_FINDING_UNROUTABLE",
          cause instanceof Error ? cause.message : String(cause),
        );
        break;
      }

      const activeCorrectionRound = correctionRound + 1;
      const correctionCommits: string[] = [];
      let correctionFailed = false;
      for (const [owner, findings] of routed) {
        const baseAssignment = baseAssignments.get(owner.name);
        if (!baseAssignment) {
          correctionFailed = true;
          break;
        }
        const resumedCorrection =
          resume &&
          resumedRoleNames.has(owner.name) &&
          owner.assignment?.assignmentId.includes("review-feedback");
        const priorCommits = new Set(
          resumedCorrection
            ? (integrationCommitsAtResume.get(owner.name) ?? [])
            : (owner.integrationCommits ?? []),
        );
        let correction = resumedCorrection
          ? terminalByRole.get(owner.name)
          : undefined;
        if (!resumedCorrection) {
          try {
            owner.assignment = correctionAssignment(
              baseAssignment,
              findings,
              activeCorrectionRound,
            );
          } catch (cause) {
            await emitReviewError(
              "REVIEW_CORRECTION_ASSIGNMENT_INVALID",
              cause instanceof Error ? cause.message : String(cause),
              owner.name,
            );
            correctionFailed = true;
            break;
          }
          correction = await launchRoleWithAutomaticRetry(owner, {
            reuseWorktree: true,
            logSuffix: `review-feedback-${activeCorrectionRound}`,
          });
        }
        if (!correction) {
          correctionFailed = true;
          break;
        }
        terminalByRole.set(owner.name, correction);
        if (correction.status !== "completed") {
          correctionFailed = true;
          break;
        }
        if (!correction.files?.length) {
          await emitReviewError(
            "REVIEW_CORRECTION_NO_CHANGES",
            `${owner.name} completed correction round ${activeCorrectionRound} without Conduit-observed file changes.`,
            owner.name,
          );
          correctionFailed = true;
          break;
        }
        correctionCommits.push(
          ...(owner.integrationCommits ?? []).filter(
            (commit) => !priorCommits.has(commit),
          ),
        );
      }
      if (correctionFailed || !correctionCommits.length) break;

      correctionRound = activeCorrectionRound;
      reviewerWorkflow.correctionRound = correctionRound;
      seenReviews.add(fingerprint);
      reviewerWorkflow.findingFingerprints = [...seenReviews].slice(-10);
      await onRoleWorkspaceReady?.();

      const reviewerBase = baseAssignments.get(reviewerRole.name);
      if (!reviewerBase) break;
      try {
        reviewerRole.assignment = reReviewAssignment(
          reviewerBase,
          correctionRound,
        );
      } catch (cause) {
        await emitReviewError(
          "REVIEW_CORRECTION_ASSIGNMENT_INVALID",
          cause instanceof Error ? cause.message : String(cause),
        );
        break;
      }
      reviewerResult = await launchRoleWithAutomaticRetry(reviewerRole, {
        reuseWorktree: true,
        additionalCommits: correctionCommits,
        logSuffix: `review-${correctionRound}`,
      });
      terminalByRole.set(reviewerRole.name, reviewerResult);
    }
  }
  const latestResults = run.roles.map((role) => {
    const result = terminalByRole.get(role.name) ?? {
      role: role.name,
      status: "failed" as const,
      error: "Role did not produce a terminal result.",
    };
    if (role === reviewerRole && !reviewGateApproved)
      return {
        ...result,
        status: "failed" as const,
        error:
          result.resultRecord?.response.verdict?.rationale ??
          "Reviewer did not approve the workflow.",
      };
    return result;
  });
  if (reviewGateApproved && reviewerRole) {
    try {
      await anchorRoleLineageInReviewer({
        projectRoot,
        run,
        reviewer: reviewerRole,
        repository: roleWorkspaceRepository,
      });
      promoteApprovedReviewerWorktree({
        projectRoot,
        reviewer: reviewerRole,
        expectedProjectHead: projectHeadAtStart!,
        disabledHooksDirectory: runDisabledHooksDirectory,
        stateDirectory:
          run.stateDirectory ?? path.join(projectRoot, ".conduit"),
      });
      await eventRepository?.append({
        type: "activity",
        provenance: RunnerEventProvenance.ConduitObserved,
        runId: run.id,
        roleId: "system",
        timestamp: new Date().toISOString(),
        payload: {
          kind: "activity",
          message: "Approved reviewer worktree integrated into the project.",
        },
      });
    } catch (cause) {
      reviewGateApproved = false;
      await emitReviewError(
        "APPROVED_WORKTREE_INTEGRATION_FAILED",
        cause instanceof Error ? cause.message : String(cause),
        "system",
      );
    }
  }
  const terminalStatus = terminalRunStatus(latestResults, reviewGateApproved);
  run.status = terminalStatus;
  for (const role of run.roles)
    role.status =
      (latestResults.find((result) => result.role === role.name)
        ?.status as RunRole["status"]) ?? "failed";
  const promotedHead =
    terminalStatus === "completed" && reviewerRole
      ? repositoryHead(projectRoot)
      : undefined;
  if (roleWorkspaceRepository && promotedHead) {
    for (const role of run.roles) {
      if (!roleWorkspaceLeaseIdentity(run.id, role)) continue;
      try {
        await cleanupPromotedRoleWorkspace({
          projectRoot,
          runId: run.id,
          role,
          promotedHead,
          repository: roleWorkspaceRepository,
        });
      } catch (cause) {
        const identity = roleWorkspaceLeaseIdentity(run.id, role);
        if (identity) {
          const slot = await roleWorkspaceRepository.load(
            identity.repositoryId,
            identity.roleKey,
          );
          if (slot && slot.state !== RoleWorkspaceState.CleanupPending)
            await roleWorkspaceRepository.transition(
              identity,
              slot.state,
              RoleWorkspaceState.CleanupPending,
            );
        }
        await emitReviewError(
          "ROLE_WORKSPACE_CLEANUP_PENDING",
          cause instanceof Error ? cause.message : String(cause),
          role.name,
        );
      }
    }
  } else if (roleWorkspaceRepository) {
    for (const role of run.roles) {
      const identity = roleWorkspaceLeaseIdentity(run.id, role);
      if (!identity) continue;
      const slot = await roleWorkspaceRepository.load(
        identity.repositoryId,
        identity.roleKey,
      );
      if (
        slot?.owningRunId === run.id &&
        slot.state === RoleWorkspaceState.Retained &&
        !slot.leaseOwner
      )
        continue;
      const retained = await roleWorkspaceRepository.retain(
        identity,
        role.worktreeHead ?? run.startingHead ?? "",
      );
      if (!retained)
        await emitReviewError(
          "ROLE_WORKSPACE_RETAIN_FAILED",
          `Role ${role.name} workspace could not be retained with its fenced identity.`,
          role.name,
        );
    }
  }
  const worktrees = run.roles
    .map((role) => role.worktree)
    .filter((worktree): worktree is string => Boolean(worktree));
  const slotManaged =
    sharedReadOnlyWorkspace ||
    run.roles.some((role) => role.workspaceRepositoryId);
  if (run.stateDirectory && worktrees.length > 0 && !slotManaged) {
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

  return latestResults;
}
