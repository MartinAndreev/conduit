import { test } from "bun:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { hashFeaturePackage } from "../../src/domains/features/services/feature-package-hasher.js";
import { evaluateRunResumeEligibility } from "../../src/domains/runs/services/run-resume-eligibility-service.js";
import type { Run } from "../../src/domains/runs/types/run.js";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";
import { TursoRoleWorkspaceRepository } from "../../src/domains/runs/repositories/turso-role-workspace-repository.js";
import { resolveRepositoryIdentity } from "../../src/domains/runs/services/role-workspace-identity-service.js";
import type { ConduitResultRecordRepository } from "../../src/domains/runs/interfaces/conduit-result-record-repository.js";

async function fixture(): Promise<{ root: string; run: Run }> {
  const root = await mkdtemp(
    path.join(tmpdir(), "conduit-resume-eligibility-"),
  );
  execFileSync("git", ["-C", root, "init"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Test"]);
  const packageRoot = path.join(root, "specs", "008-feature");
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
  const packageHash = (
    await hashFeaturePackage({
      packageRoot,
      ownershipInputs: [
        { role: "reviewer", readOnly: true, owns: [], dependsOn: [] },
      ],
    })
  ).hash;
  return {
    root,
    run: {
      id: "run-resume",
      featureId: "008",
      status: "failed",
      createdAt: "2026-01-01T00:00:00.000Z",
      startingHead: head,
      featurePackageHash: packageHash,
      featurePackagePath: "specs/008-feature",
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
          worktree: root,
          worktreeHead: head,
        },
      ],
    },
  };
}

test("resume eligibility verifies identity and reports preserved/retry roles", async () => {
  const { root, run } = await fixture();
  try {
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
    });
    assert.equal(eligibility.state, "resumable");
    assert.deepEqual(eligibility.retryRoles, ["reviewer"]);
    assert.deepEqual(eligibility.preservedRoles, []);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("an approved completed reviewer can resume promotion without rerunning roles", async () => {
  const { root, run } = await fixture();
  try {
    run.roles[0]!.status = "completed";
    const resultRepository = {
      save: async () => {},
      load: async () => ({
        recordVersion: "1.0" as const,
        runId: run.id,
        featureId: run.featureId,
        taskId: null,
        assignmentId: `${run.id}:reviewer`,
        role: "reviewer",
        runner: "codex",
        model: null,
        receivedAt: new Date().toISOString(),
        process: { exitCode: 0, acceptable: true, cancelled: false },
        observedChangedFiles: [],
        conduitObservedEvents: [],
        runnerReportedEvents: [],
        agentClaimedEvents: [],
        protocolValidation: { valid: true, issues: [] },
        semanticValidation: { valid: true, issues: [] },
        response: {
          protocolVersion: "1.0" as const,
          status: "completed" as const,
          summary: "approved",
          verdict: { decision: "approved" as const, rationale: "verified" },
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
        },
      }),
    } satisfies ConduitResultRecordRepository;
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
      resultRepository,
    });
    assert.equal(eligibility.state, "resumable");
    assert.deepEqual(eligibility.preservedRoles, ["reviewer"]);
    assert.deepEqual(eligibility.retryRoles, []);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("protocol-failed workspaces remain resumable with checkpointable changes", async () => {
  const { root, run } = await fixture();
  try {
    run.roles[0]!.lastFailureKind = "structural-response";
    await mkdir(path.join(root, "dist", "assets"), { recursive: true });
    await writeFile(
      path.join(root, "dist", "assets", "generated.js"),
      "generated\n",
    );
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
    });
    assert.equal(eligibility.state, "resumable");
    assert.deepEqual(eligibility.retryRoles, ["reviewer"]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("resume eligibility schedules an unstarted downstream role without a workspace", async () => {
  const { root, run } = await fixture();
  try {
    run.roles.push({
      name: "publish",
      runner: "codex",
      readOnly: false,
      owns: ["release"],
      dependsOn: ["reviewer"],
      promptFile: "",
      prompt: "",
      command: "",
      args: [],
      skillSource: "test",
      status: "failed",
    });
    run.featurePackageHash = (
      await hashFeaturePackage({
        packageRoot: path.join(root, run.featurePackagePath!),
        ownershipInputs: run.roles.map((role) => ({
          role: role.name,
          readOnly: role.readOnly,
          owns: role.owns,
          dependsOn: role.dependsOn,
        })),
      })
    ).hash;
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
    });
    assert.equal(eligibility.state, "resumable");
    assert.deepEqual(eligibility.retryRoles, ["reviewer", "publish"]);
    assert.deepEqual(eligibility.reconstructRoles, ["publish"]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a proven provisioning crash window is reconstructable", async () => {
  const { root, run } = await fixture();
  const connection = await new ProjectDatabaseFactory(root).open();
  const workspace = `${root}-reviewer-slot`;
  try {
    const repositoryId = resolveRepositoryIdentity(root).repositoryId;
    execFileSync("git", [
      "-C",
      root,
      "worktree",
      "add",
      "-b",
      "conduit/run-resume/reviewer",
      workspace,
      "HEAD",
    ]);
    const role = run.roles[0]!;
    role.worktree = workspace;
    role.worktreeHead = undefined;
    role.workspaceRepositoryId = repositoryId;
    role.workspaceRoleKey = "reviewer";
    role.workspaceBranchName = "conduit/run-resume/reviewer";
    role.workspaceAssignmentHash = "d".repeat(64);
    role.workspaceLeaseOwner = "run-resume:reviewer";
    const slots = new TursoRoleWorkspaceRepository(connection);
    assert.equal(
      (
        await slots.claim({
          repositoryId,
          roleKey: "reviewer",
          workspacePath: workspace,
          owningRunId: run.id,
          startingHead: run.startingHead!,
          packageHash: run.featurePackageHash!,
          assignmentHash: role.workspaceAssignmentHash,
          branchName: role.workspaceBranchName,
          leaseOwner: role.workspaceLeaseOwner,
        })
      ).status,
      "claimed",
    );
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
      roleWorkspaceRepository: slots,
    });
    assert.equal(eligibility.state, "resumable");
    assert.deepEqual(eligibility.reconstructRoles, ["reviewer"]);
  } finally {
    await connection.close();
    await rm(root, { recursive: true, force: true });
    await rm(workspace, { recursive: true, force: true });
  }
});

test("an unstarted downstream role retained without a worktree is reconstructable", async () => {
  const { root, run } = await fixture();
  const connection = await new ProjectDatabaseFactory(root).open();
  const workspace = `${root}-unstarted-reviewer-slot`;
  try {
    const repositoryId = resolveRepositoryIdentity(root).repositoryId;
    const role = run.roles[0]!;
    role.worktree = workspace;
    role.worktreeHead = undefined;
    role.workspaceRepositoryId = repositoryId;
    role.workspaceRoleKey = "reviewer";
    role.workspaceBranchName = "conduit/run-resume/reviewer";
    role.workspaceAssignmentHash = "e".repeat(64);
    role.workspaceLeaseOwner = "run-resume:reviewer";
    role.workspaceFencingToken = 1;
    const slots = new TursoRoleWorkspaceRepository(connection);
    assert.equal(
      (
        await slots.claim({
          repositoryId,
          roleKey: "reviewer",
          workspacePath: workspace,
          owningRunId: run.id,
          startingHead: run.startingHead!,
          packageHash: run.featurePackageHash!,
          assignmentHash: role.workspaceAssignmentHash,
          branchName: role.workspaceBranchName,
          leaseOwner: role.workspaceLeaseOwner,
        })
      ).status,
      "claimed",
    );
    assert.equal(
      await slots.retain(
        {
          repositoryId,
          roleKey: "reviewer",
          owningRunId: run.id,
          leaseOwner: role.workspaceLeaseOwner,
          fencingToken: role.workspaceFencingToken,
        },
        run.startingHead!,
      ),
      true,
    );
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
      roleWorkspaceRepository: slots,
    });
    assert.equal(eligibility.state, "resumable");
    assert.deepEqual(eligibility.reconstructRoles, ["reviewer"]);
  } finally {
    await connection.close();
    await rm(root, { recursive: true, force: true });
    await rm(workspace, { recursive: true, force: true });
  }
});

test("production resume rejects legacy roles without canonical workspace metadata", async () => {
  const { root, run } = await fixture();
  const connection = await new ProjectDatabaseFactory(root).open();
  try {
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
      roleWorkspaceRepository: new TursoRoleWorkspaceRepository(connection),
    });
    assert.equal(eligibility.state, "not-resumable");
    assert.match(eligibility.reason ?? "", /canonical workspace metadata/i);
  } finally {
    await connection.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("resume eligibility fails closed when the feature package changes", async () => {
  const { root, run } = await fixture();
  try {
    await writeFile(
      path.join(root, "specs", "008-feature", "spec.md"),
      "changed\n",
    );
    const eligibility = await evaluateRunResumeEligibility({
      projectRoot: root,
      run,
    });
    assert.equal(eligibility.state, "not-resumable");
    assert.match(eligibility.reason ?? "", /package changed/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
