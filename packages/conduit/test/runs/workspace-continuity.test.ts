import { test } from "bun:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { hashFeaturePackage } from "../../src/domains/features/services/feature-package-hasher.js";
import { RoleWorkspaceState } from "../../src/domains/runs/enums/role-workspace-state.js";
import type { RoleWorkspaceRepository } from "../../src/domains/runs/interfaces/role-workspace-repository.js";
import type { RunRecoveryRepository } from "../../src/domains/runs/interfaces/run-recovery-repository.js";
import { evaluateWorkspaceContinuity } from "../../src/domains/runs/services/workspace-continuity-service.js";
import { resolveRepositoryIdentity } from "../../src/domains/runs/services/role-workspace-identity-service.js";
import type { Run } from "../../src/domains/runs/types/run.js";
import type { RoleWorkspaceSlot } from "../../src/domains/runs/types/role-workspace.js";

async function fixture() {
  const root = await mkdtemp(path.join(tmpdir(), "conduit-continuity-"));
  execFileSync("git", ["-C", root, "init"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Test"]);
  const packageRoot = path.join(root, "specs", "009-feature");
  await mkdir(packageRoot, { recursive: true });
  await writeFile(path.join(packageRoot, "spec.md"), "approved\n");
  await writeFile(path.join(root, "README.md"), "base\n");
  execFileSync("git", ["-C", root, "add", "."]);
  execFileSync("git", [
    "-C",
    root,
    "-c",
    "commit.gpgSign=false",
    "commit",
    "-m",
    "base",
  ]);
  const head = execFileSync("git", ["-C", root, "rev-parse", "HEAD"], {
    encoding: "utf8",
  }).trim();
  const workspace = path.join(
    path.dirname(root),
    `${path.basename(root)}-reviewer`,
  );
  execFileSync("git", [
    "-C",
    root,
    "worktree",
    "add",
    "-b",
    "conduit/run-1/reviewer",
    workspace,
  ]);
  const packageHash = (
    await hashFeaturePackage({
      packageRoot,
      ownershipInputs: [
        { role: "reviewer", readOnly: true, owns: [], dependsOn: [] },
      ],
    })
  ).hash;
  const repositoryId = resolveRepositoryIdentity(root).repositoryId;
  const run: Run = {
    id: "run-1",
    featureId: "009",
    status: "failed",
    createdAt: new Date().toISOString(),
    startingHead: head,
    featurePackageHash: packageHash,
    featurePackagePath: "specs/009-feature",
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
        status: "failed",
        worktree: workspace,
        worktreeHead: head,
        workspaceRepositoryId: repositoryId,
        workspaceRoleKey: "reviewer",
        workspaceBranchName: "conduit/run-1/reviewer",
        workspaceAssignmentHash: "a".repeat(64),
        workspaceLeaseOwner: "run-1:reviewer",
      },
    ],
  };
  const slot: RoleWorkspaceSlot = {
    repositoryId,
    roleKey: "reviewer",
    generation: 1,
    workspacePath: workspace,
    owningRunId: run.id,
    state: RoleWorkspaceState.Retained,
    startingHead: head,
    packageHash,
    assignmentHash: "a".repeat(64),
    worktreeHead: head,
    branchName: "conduit/run-1/reviewer",
    fencingToken: 1,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  };
  return { root, workspace, run, slot };
}

function repositories(run: Run, slot: RoleWorkspaceSlot) {
  const recovery = {
    loadSnapshot: async () => ({
      run,
      state: "interrupted" as const,
      version: 1,
      updatedAt: new Date().toISOString(),
    }),
    saveSnapshot: async () => {
      throw new Error("unused");
    },
    claimFailedRun: async () => undefined,
    listSnapshots: async () => [],
    markInterrupted: async () => {},
    markCancelled: async () => {},
  } satisfies RunRecoveryRepository;
  const workspaces = {
    load: async (_repositoryId, roleKey) =>
      roleKey === slot.roleKey ? slot : undefined,
    claim: async () => {
      throw new Error("unused");
    },
    claimAll: async () => {
      throw new Error("unused");
    },
    advanceAll: async () => {
      throw new Error("unused");
    },
    recordHead: async () => false,
    retain: async () => false,
    transition: async () => false,
    completeGeneration: async () => false,
    remove: async () => false,
    listByRun: async () => [slot],
    listCleanupCandidates: async () => [],
    listGenerations: async () => [],
  } satisfies RoleWorkspaceRepository;
  return { recovery, workspaces };
}

test("workspace continuity reports compatible retained work", async () => {
  const value = await fixture();
  try {
    const repos = repositories(value.run, value.slot);
    const continuity = await evaluateWorkspaceContinuity({
      projectRoot: value.root,
      featureId: "009",
      roleNames: ["reviewer"],
      recoveryRepository: repos.recovery,
      roleWorkspaceRepository: repos.workspaces,
    });
    assert.equal(continuity.state, "compatible-continue");
    if (continuity.state === "compatible-continue")
      assert.deepEqual(continuity.retryRoles, ["reviewer"]);
  } finally {
    await rm(value.root, { recursive: true, force: true });
    await rm(value.workspace, { recursive: true, force: true });
  }
});

test("workspace continuity treats a failed run lease as recoverable", async () => {
  const value = await fixture();
  try {
    value.run.roles[0]!.workspaceFencingToken = 1;
    const repos = repositories(value.run, {
      ...value.slot,
      leaseOwner: "run-1:reviewer",
    });
    const continuity = await evaluateWorkspaceContinuity({
      projectRoot: value.root,
      featureId: "009",
      roleNames: ["reviewer"],
      recoveryRepository: repos.recovery,
      roleWorkspaceRepository: repos.workspaces,
    });
    assert.equal(continuity.state, "compatible-continue");
  } finally {
    await rm(value.root, { recursive: true, force: true });
    await rm(value.workspace, { recursive: true, force: true });
  }
});

test("workspace continuity rejects a foreign lease on a failed run", async () => {
  const value = await fixture();
  try {
    const repos = repositories(value.run, {
      ...value.slot,
      leaseOwner: "foreign-process",
    });
    const continuity = await evaluateWorkspaceContinuity({
      projectRoot: value.root,
      featureId: "009",
      roleNames: ["reviewer"],
      recoveryRepository: repos.recovery,
      roleWorkspaceRepository: repos.workspaces,
    });
    assert.equal(continuity.state, "lease-conflict");
  } finally {
    await rm(value.root, { recursive: true, force: true });
    await rm(value.workspace, { recursive: true, force: true });
  }
});

test("workspace continuity reports active lease conflicts", async () => {
  const value = await fixture();
  try {
    value.run.status = "running";
    const repos = repositories(value.run, {
      ...value.slot,
      leaseOwner: "run-1:reviewer",
    });
    const continuity = await evaluateWorkspaceContinuity({
      projectRoot: value.root,
      featureId: "009",
      roleNames: ["reviewer", "qa"],
      recoveryRepository: repos.recovery,
      roleWorkspaceRepository: repos.workspaces,
    });
    assert.equal(continuity.state, "lease-conflict");
  } finally {
    await rm(value.root, { recursive: true, force: true });
    await rm(value.workspace, { recursive: true, force: true });
  }
});
