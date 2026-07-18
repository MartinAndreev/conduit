import { test } from "bun:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import type { RoleWorkspaceRepository } from "../../src/domains/runs/interfaces/role-workspace-repository.js";
import { TursoRoleWorkspaceRepository } from "../../src/domains/runs/repositories/turso-role-workspace-repository.js";
import { resolveRepositoryIdentity } from "../../src/domains/runs/services/role-workspace-identity-service.js";
import { startNewRoleWorkspaces } from "../../src/domains/runs/services/start-new-role-workspaces-service.js";
import type { Run } from "../../src/domains/runs/types/run.js";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";

function role(
  runId: string,
  repositoryId: string,
  workspace: string,
  name = "reviewer",
): Run["roles"][number] {
  return {
    name,
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
    workspaceRepositoryId: repositoryId,
    workspaceRoleKey: name,
    workspaceBranchName: `conduit/${runId}/${name}`,
    workspaceAssignmentHash: `${runId}:${name}`.padEnd(64, "a").slice(0, 64),
    workspaceLeaseOwner: `${runId}:${name}`,
  };
}

test("Start Anew preserves old branch lineage and advances the role slot", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "conduit-start-new-"));
  execFileSync("git", ["-C", root, "init"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Test"]);
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
  const connection = await new ProjectDatabaseFactory(root).open();
  try {
    const repositoryId = resolveRepositoryIdentity(root).repositoryId;
    const workspace = path.join(root, ".slots", repositoryId, "reviewer");
    execFileSync("git", [
      "-C",
      root,
      "worktree",
      "add",
      "-b",
      "conduit/old-run/reviewer",
      workspace,
    ]);
    const repository = new TursoRoleWorkspaceRepository(connection);
    const oldRole = role("old-run", repositoryId, workspace);
    oldRole.worktreeHead = head;
    oldRole.linkedWorkspacePaths = ["dependencies"];
    await mkdir(path.join(workspace, "dependencies", "cache"), {
      recursive: true,
    });
    await writeFile(
      path.join(workspace, "dependencies", "cache", "fixture"),
      "generated\n",
    );
    const claimed = await repository.claim({
      repositoryId,
      roleKey: "reviewer",
      workspacePath: workspace,
      owningRunId: "old-run",
      startingHead: head,
      packageHash: "b".repeat(64),
      assignmentHash: oldRole.workspaceAssignmentHash!,
      branchName: oldRole.workspaceBranchName!,
      leaseOwner: oldRole.workspaceLeaseOwner!,
    });
    assert.equal(claimed.status, "claimed");
    if (claimed.status !== "claimed") return;
    const identity = {
      repositoryId,
      roleKey: "reviewer",
      owningRunId: "old-run",
      leaseOwner: oldRole.workspaceLeaseOwner!,
      fencingToken: claimed.slot.fencingToken,
    };
    assert.equal(await repository.recordHead(identity, head), true);
    assert.equal(await repository.retain(identity, head), true);
    const newWorkspace = path.join(root, ".slots", repositoryId, "qa");
    const oldUnmaterializedRole = role(
      "old-run",
      repositoryId,
      newWorkspace,
      "qa",
    );
    const unmaterializedClaim = await repository.claim({
      repositoryId,
      roleKey: "qa",
      workspacePath: newWorkspace,
      owningRunId: "old-run",
      startingHead: head,
      packageHash: "b".repeat(64),
      assignmentHash: oldUnmaterializedRole.workspaceAssignmentHash!,
      branchName: oldUnmaterializedRole.workspaceBranchName!,
      leaseOwner: oldUnmaterializedRole.workspaceLeaseOwner!,
    });
    assert.equal(unmaterializedClaim.status, "claimed");
    if (unmaterializedClaim.status !== "claimed") return;
    const unmaterializedIdentity = {
      repositoryId,
      roleKey: "qa",
      owningRunId: "old-run",
      leaseOwner: oldUnmaterializedRole.workspaceLeaseOwner!,
      fencingToken: unmaterializedClaim.slot.fencingToken,
    };
    assert.equal(
      await repository.recordHead(unmaterializedIdentity, head),
      true,
    );
    assert.equal(await repository.retain(unmaterializedIdentity, head), true);
    const previousRun: Run = {
      id: "old-run",
      featureId: "008",
      status: "failed",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "b".repeat(64),
      roles: [oldRole, oldUnmaterializedRole],
    };
    const nextRole = role("new-run", repositoryId, workspace);
    nextRole.status = "planned";
    const newRole = {
      ...role("new-run", repositoryId, newWorkspace),
      name: "qa",
      workspaceRoleKey: "qa",
      workspaceBranchName: "conduit/new-run/qa",
      workspaceAssignmentHash: "q".repeat(64),
      workspaceLeaseOwner: "new-run:qa",
      status: "planned" as const,
    };
    const absentWorkspace = path.join(root, ".slots", repositoryId, "backend");
    const absentRole = role(
      "new-run",
      repositoryId,
      absentWorkspace,
      "backend",
    );
    absentRole.status = "planned";
    const nextRun: Run = {
      id: "new-run",
      featureId: "009",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "c".repeat(64),
      roles: [nextRole, newRole, absentRole],
    };
    await startNewRoleWorkspaces({
      projectRoot: root,
      previousRuns: [previousRun],
      nextRun,
      repository,
    });
    assert.equal(
      execFileSync(
        "git",
        ["-C", root, "rev-parse", "refs/heads/conduit/old-run/reviewer"],
        { encoding: "utf8" },
      ).trim(),
      head,
    );
    const slot = await repository.load(repositoryId, "reviewer");
    assert.equal(slot?.generation, 2);
    assert.equal(slot?.owningRunId, "new-run");
    assert.equal(slot?.branchName, "conduit/new-run/reviewer");
    assert.equal((await repository.load(repositoryId, "qa"))?.generation, 2);
    assert.equal(
      execFileSync(
        "git",
        ["-C", root, "rev-parse", "refs/heads/conduit/old-run/qa"],
        { encoding: "utf8" },
      ).trim(),
      head,
    );
    assert.equal(await repository.load(repositoryId, "backend"), undefined);
    const generations = await repository.listGenerations(
      repositoryId,
      "reviewer",
    );
    assert.equal(generations[0]?.outcome, "abandoned");
    assert.equal(generations[0]?.branchOid, head);
    assert.equal(generations[1]?.outcome, undefined);
  } finally {
    await connection.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("Start Anew reconciles an exact stale registration after its worktree was deleted", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "conduit-start-new-stale-"));
  execFileSync("git", ["-C", root, "init"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Test"]);
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
  const connection = await new ProjectDatabaseFactory(root).open();
  try {
    const repositoryId = resolveRepositoryIdentity(root).repositoryId;
    const workspace = path.join(root, ".slots", repositoryId, "qa");
    execFileSync("git", [
      "-C",
      root,
      "worktree",
      "add",
      "-b",
      "conduit/old/qa",
      workspace,
    ]);
    const repository = new TursoRoleWorkspaceRepository(connection);
    const previousRole = role("old", repositoryId, workspace, "qa");
    previousRole.worktreeHead = head;
    const claimed = await repository.claim({
      repositoryId,
      roleKey: "qa",
      workspacePath: workspace,
      owningRunId: "old",
      startingHead: head,
      packageHash: "b".repeat(64),
      assignmentHash: previousRole.workspaceAssignmentHash!,
      branchName: previousRole.workspaceBranchName!,
      leaseOwner: previousRole.workspaceLeaseOwner!,
    });
    assert.equal(claimed.status, "claimed");
    if (claimed.status !== "claimed") return;
    const identity = {
      repositoryId,
      roleKey: "qa",
      owningRunId: "old",
      leaseOwner: previousRole.workspaceLeaseOwner!,
      fencingToken: claimed.slot.fencingToken,
    };
    assert.equal(await repository.recordHead(identity, head), true);
    assert.equal(await repository.retain(identity, head), true);
    await rm(workspace, { recursive: true, force: true });
    assert.match(
      execFileSync("git", ["-C", root, "worktree", "list", "--porcelain"], {
        encoding: "utf8",
      }),
      new RegExp(
        `worktree ${workspace.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`,
      ),
    );
    const previousRun: Run = {
      id: "old",
      featureId: "008",
      status: "failed",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "b".repeat(64),
      roles: [previousRole],
    };
    const nextRole = role("new", repositoryId, workspace, "qa");
    nextRole.status = "planned";
    const nextRun: Run = {
      id: "new",
      featureId: "009",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "c".repeat(64),
      roles: [nextRole],
    };

    await startNewRoleWorkspaces({
      projectRoot: root,
      previousRuns: [previousRun],
      nextRun,
      repository,
    });

    const worktrees = execFileSync(
      "git",
      ["-C", root, "worktree", "list", "--porcelain"],
      { encoding: "utf8" },
    );
    assert.equal(worktrees.includes(`worktree ${workspace}`), false);
    assert.equal(
      (await repository.listGenerations(repositoryId, "qa"))[0]?.outcome,
      "abandoned",
    );
    assert.equal(
      (await repository.load(repositoryId, "qa"))?.owningRunId,
      "new",
    );
  } finally {
    await connection.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("Start Anew recovers failed provisioning blocked by a prunable legacy registration", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "conduit-start-new-legacy-"));
  execFileSync("git", ["-C", root, "init"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Test"]);
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
  const connection = await new ProjectDatabaseFactory(root).open();
  try {
    const repositoryId = resolveRepositoryIdentity(root).repositoryId;
    const workspace = path.join(root, ".slots", repositoryId, "frontend");
    execFileSync("git", [
      "-C",
      root,
      "worktree",
      "add",
      "-b",
      "conduit/legacy/frontend",
      workspace,
    ]);
    await rm(workspace, { recursive: true, force: true });

    const repository = new TursoRoleWorkspaceRepository(connection);
    const previousRole = role(
      "blocked-run",
      repositoryId,
      workspace,
      "frontend",
    );
    const claimed = await repository.claim({
      repositoryId,
      roleKey: "frontend",
      workspacePath: workspace,
      owningRunId: "blocked-run",
      startingHead: head,
      packageHash: "b".repeat(64),
      assignmentHash: previousRole.workspaceAssignmentHash!,
      branchName: previousRole.workspaceBranchName!,
      leaseOwner: previousRole.workspaceLeaseOwner!,
    });
    assert.equal(claimed.status, "claimed");
    if (claimed.status !== "claimed") return;
    assert.equal(claimed.slot.state, "provisioning");
    assert.equal(claimed.slot.worktreeHead, undefined);
    const previousRun: Run = {
      id: "blocked-run",
      featureId: "008",
      status: "failed",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "b".repeat(64),
      roles: [previousRole],
    };
    const nextRole = role("new-run", repositoryId, workspace, "frontend");
    nextRole.status = "planned";
    const nextRun: Run = {
      id: "new-run",
      featureId: "009",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "c".repeat(64),
      roles: [nextRole],
    };

    await startNewRoleWorkspaces({
      projectRoot: root,
      previousRuns: [previousRun],
      nextRun,
      repository,
    });

    const listed = execFileSync(
      "git",
      ["-C", root, "worktree", "list", "--porcelain"],
      { encoding: "utf8" },
    );
    assert.equal(listed.includes(`worktree ${workspace}`), false);
    assert.equal(
      execFileSync(
        "git",
        ["-C", root, "rev-parse", "refs/heads/conduit/legacy/frontend"],
        { encoding: "utf8" },
      ).trim(),
      head,
    );
    assert.equal(
      (await repository.load(repositoryId, "frontend"))?.owningRunId,
      "new-run",
    );
    assert.equal(
      (await repository.listGenerations(repositoryId, "frontend"))[0]
        ?.branchOid,
      head,
    );
  } finally {
    await connection.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("Start Anew preflights every role before mutating any workspace", async () => {
  const root = await mkdtemp(
    path.join(tmpdir(), "conduit-start-new-preflight-"),
  );
  execFileSync("git", ["-C", root, "init"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Test"]);
  await writeFile(path.join(root, "README.md"), "base\n");
  await writeFile(path.join(root, ".gitignore"), "*.cache\n");
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
  const connection = await new ProjectDatabaseFactory(root).open();
  try {
    const repositoryId = resolveRepositoryIdentity(root).repositoryId;
    const repository = new TursoRoleWorkspaceRepository(connection);
    const names = ["worker", "reviewer"];
    const previousRoles: Run["roles"] = [];
    const nextRoles: Run["roles"] = [];
    for (const name of names) {
      const workspace = path.join(root, ".slots", repositoryId, name);
      execFileSync("git", [
        "-C",
        root,
        "worktree",
        "add",
        "-b",
        `conduit/old/${name}`,
        workspace,
      ]);
      const previousRole = role("old", repositoryId, workspace, name);
      previousRole.worktreeHead = head;
      previousRoles.push(previousRole);
      nextRoles.push(role("new", repositoryId, workspace, name));
      const claimed = await repository.claim({
        repositoryId,
        roleKey: name,
        workspacePath: workspace,
        owningRunId: "old",
        startingHead: head,
        packageHash: "b".repeat(64),
        assignmentHash: previousRole.workspaceAssignmentHash!,
        branchName: previousRole.workspaceBranchName!,
        leaseOwner: previousRole.workspaceLeaseOwner!,
      });
      assert.equal(claimed.status, "claimed");
      if (claimed.status !== "claimed") return;
      const identity = {
        repositoryId,
        roleKey: name,
        owningRunId: "old",
        leaseOwner: previousRole.workspaceLeaseOwner!,
        fencingToken: claimed.slot.fencingToken,
      };
      assert.equal(await repository.recordHead(identity, head), true);
      assert.equal(await repository.retain(identity, head), true);
    }
    const previousRun: Run = {
      id: "old",
      featureId: "008",
      status: "failed",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "b".repeat(64),
      roles: previousRoles,
    };
    const nextRun: Run = {
      id: "new",
      featureId: "009",
      status: "planned",
      createdAt: new Date().toISOString(),
      startingHead: head,
      featurePackageHash: "c".repeat(64),
      roles: nextRoles,
    };
    const assertUntouched = async () => {
      assert.equal(existsSync(previousRoles[0]!.worktree!), true);
      assert.equal(
        (await repository.listGenerations(repositoryId, "worker"))[0]?.outcome,
        undefined,
      );
      assert.equal(
        (await repository.load(repositoryId, "worker"))?.leaseOwner,
        undefined,
      );
    };
    const attempt = () =>
      startNewRoleWorkspaces({
        projectRoot: root,
        previousRuns: [previousRun],
        nextRun,
        repository,
      });

    const laterWorkspace = previousRoles[1]!.worktree!;
    const dirtyPath = path.join(laterWorkspace, "dirty.tmp");
    await writeFile(dirtyPath, "dirty");
    await assert.rejects(attempt, /uncheckpointed workspace data/);
    await assertUntouched();
    await rm(dirtyPath, { force: true });

    const ignoredPath = path.join(laterWorkspace, "ignored.cache");
    await writeFile(ignoredPath, "ignored");
    await assert.rejects(attempt, /uncheckpointed workspace data/);
    await assertUntouched();
    await rm(ignoredPath, { force: true });

    execFileSync("git", ["-C", root, "worktree", "lock", laterWorkspace]);
    await assert.rejects(attempt, /worktree registration is locked/);
    await assertUntouched();
    execFileSync("git", ["-C", root, "worktree", "unlock", laterWorkspace]);

    await writeFile(path.join(laterWorkspace, "README.md"), "modified\n");
    await assert.rejects(attempt, /uncheckpointed workspace data/);
    await assertUntouched();
    execFileSync("git", ["-C", laterWorkspace, "checkout", "--", "README.md"]);

    const mergeHead = execFileSync(
      "git",
      ["-C", laterWorkspace, "rev-parse", "--git-path", "MERGE_HEAD"],
      { encoding: "utf8" },
    ).trim();
    await writeFile(path.resolve(laterWorkspace, mergeHead), `${head}\n`);
    await assert.rejects(attempt, /in-progress Git operation/);
    await assertUntouched();
    await rm(path.resolve(laterWorkspace, mergeHead), { force: true });

    const originalPath = nextRoles[1]!.worktree;
    nextRoles[1]!.worktree = path.join(root, ".slots", repositoryId, "wrong");
    await assert.rejects(attempt, /retained slot is not resettable/);
    await assertUntouched();
    nextRoles[1]!.worktree = originalPath;

    const tree = execFileSync(
      "git",
      ["-C", root, "rev-parse", `${head}^{tree}`],
      {
        encoding: "utf8",
      },
    ).trim();
    const divergentOid = execFileSync(
      "git",
      ["-C", root, "commit-tree", tree, "-p", head, "-m", "divergent"],
      { encoding: "utf8" },
    ).trim();
    execFileSync("git", [
      "-C",
      root,
      "update-ref",
      "refs/heads/conduit/old/reviewer",
      divergentOid,
      head,
    ]);
    await assert.rejects(attempt, /branch checkpoint diverged/);
    await assertUntouched();
    execFileSync("git", [
      "-C",
      root,
      "update-ref",
      "refs/heads/conduit/old/reviewer",
      head,
      divergentOid,
    ]);
    execFileSync("git", ["-C", laterWorkspace, "reset", "--hard", head]);

    let raced = false;
    const racingRepository = new Proxy(repository, {
      get(target, property) {
        if (property === "claimAll")
          return async (
            inputs: Parameters<RoleWorkspaceRepository["claimAll"]>[0],
          ) => {
            const result = await target.claimAll(inputs);
            if (result.status === "claimed" && !raced) {
              raced = true;
              execFileSync("git", [
                "-C",
                root,
                "update-ref",
                "refs/heads/conduit/old/reviewer",
                divergentOid,
                head,
              ]);
            }
            return result;
          };
        const value = Reflect.get(target, property, target) as unknown;
        return typeof value === "function" ? value.bind(target) : value;
      },
    }) as RoleWorkspaceRepository;
    await assert.rejects(
      () =>
        startNewRoleWorkspaces({
          projectRoot: root,
          previousRuns: [previousRun],
          nextRun,
          repository: racingRepository,
        }),
      /branch checkpoint diverged/,
    );
    assert.equal(
      (await repository.load(repositoryId, "worker"))?.leaseOwner,
      undefined,
    );
    assert.equal(
      (await repository.load(repositoryId, "reviewer"))?.leaseOwner,
      undefined,
    );
    execFileSync("git", [
      "-C",
      root,
      "update-ref",
      "refs/heads/conduit/old/reviewer",
      head,
      divergentOid,
    ]);
    execFileSync("git", ["-C", laterWorkspace, "reset", "--hard", head]);

    const leased = await repository.claim({
      repositoryId,
      roleKey: "reviewer",
      workspacePath: laterWorkspace,
      owningRunId: "old",
      startingHead: head,
      packageHash: "b".repeat(64),
      assignmentHash: previousRoles[1]!.workspaceAssignmentHash!,
      branchName: previousRoles[1]!.workspaceBranchName!,
      leaseOwner: "other-owner",
    });
    assert.equal(leased.status, "claimed");
    await assert.rejects(attempt, /leased by run old/);
    await assertUntouched();
  } finally {
    await connection.close();
    await rm(root, { recursive: true, force: true });
  }
});
