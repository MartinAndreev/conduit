import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { RoleWorkspaceState } from "../../src/domains/runs/enums/role-workspace-state.js";
import { TursoRoleWorkspaceRepository } from "../../src/domains/runs/repositories/turso-role-workspace-repository.js";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";
import { createTursoKysely } from "../../src/system/storage/adapters/kysely-turso-dialect.js";
import type { RunsDatabase } from "../../src/domains/runs/interfaces/database-schema.js";
import type { RoleWorkspaceClaimInput } from "../../src/domains/runs/types/role-workspace.js";

function claimInput(
  overrides: Partial<RoleWorkspaceClaimInput> = {},
): RoleWorkspaceClaimInput {
  return {
    repositoryId: "a".repeat(64),
    roleKey: "reviewer",
    workspacePath: "/tmp/conduit-slots/reviewer",
    owningRunId: "run-1",
    startingHead: "b".repeat(40),
    packageHash: "c".repeat(64),
    assignmentHash: "d".repeat(64),
    branchName: "conduit/run-1/reviewer",
    leaseOwner: "process-1",
    ...overrides,
  };
}

test("role workspace claims are atomic, idempotent, and fenced", async () => {
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-role-slots-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRoleWorkspaceRepository(connection);
    const first = await repository.claim(claimInput());
    assert.equal(first.status, "claimed");
    if (first.status !== "claimed") return;
    assert.equal(first.slot.fencingToken, 1);

    const duplicate = await repository.claim(claimInput());
    assert.equal(duplicate.status, "claimed");
    if (duplicate.status !== "claimed") return;
    assert.equal(duplicate.slot.fencingToken, 1);

    const conflict = await repository.claim(
      claimInput({ leaseOwner: "process-2" }),
    );
    assert.equal(conflict.status, "lease-conflict");

    const identity = {
      repositoryId: first.slot.repositoryId,
      roleKey: first.slot.roleKey,
      owningRunId: first.slot.owningRunId,
      leaseOwner: first.slot.leaseOwner!,
      fencingToken: first.slot.fencingToken,
    };
    assert.equal(
      await repository.transition(
        identity,
        RoleWorkspaceState.Provisioning,
        RoleWorkspaceState.Running,
      ),
      true,
    );
    assert.equal(await repository.retain(identity, "e".repeat(40)), true);
    assert.equal(
      await repository.transition(
        identity,
        RoleWorkspaceState.Retained,
        RoleWorkspaceState.Running,
      ),
      false,
    );

    const reclaimed = await repository.claim(
      claimInput({ leaseOwner: "process-2" }),
    );
    assert.equal(reclaimed.status, "claimed");
    if (reclaimed.status !== "claimed") return;
    assert.equal(reclaimed.slot.fencingToken, 2);
    const reclaimedIdentity = {
      repositoryId: reclaimed.slot.repositoryId,
      roleKey: reclaimed.slot.roleKey,
      owningRunId: reclaimed.slot.owningRunId,
      leaseOwner: reclaimed.slot.leaseOwner!,
      fencingToken: reclaimed.slot.fencingToken,
    };
    assert.equal(
      await repository.recordHead(reclaimedIdentity, "f".repeat(40)),
      true,
    );
    assert.equal(
      await repository.transition(
        reclaimedIdentity,
        RoleWorkspaceState.Retained,
        RoleWorkspaceState.Running,
      ),
      true,
    );
    const completion = {
      branchOid: "f".repeat(40),
      outcome: "promoted",
      promotionOid: "f".repeat(40),
    };
    assert.equal(
      await repository.completeGeneration(reclaimedIdentity, completion),
      true,
    );
    assert.equal(
      await repository.completeGeneration(reclaimedIdentity, completion),
      true,
    );
    assert.equal((await repository.listByRun("run-1")).length, 1);
    assert.equal(
      (await repository.listCleanupCandidates("a".repeat(64))).length,
      1,
    );
    assert.equal(
      await repository.transition(
        reclaimedIdentity,
        RoleWorkspaceState.Running,
        RoleWorkspaceState.CleanupPending,
      ),
      true,
    );
    assert.equal(
      await repository.remove(
        reclaimedIdentity,
        RoleWorkspaceState.CleanupPending,
      ),
      true,
    );
    const generations = await repository.listGenerations(
      "a".repeat(64),
      "reviewer",
    );
    assert.equal(generations.length, 1);
    assert.equal(generations[0]?.outcome, "promoted");
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("start anew atomically advances an abandoned generation", async () => {
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-role-advance-"),
  );
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRoleWorkspaceRepository(connection);
    const claimed = await repository.claim(claimInput());
    assert.equal(claimed.status, "claimed");
    if (claimed.status !== "claimed") return;
    const previous = {
      repositoryId: claimed.slot.repositoryId,
      roleKey: claimed.slot.roleKey,
      owningRunId: claimed.slot.owningRunId,
      leaseOwner: claimed.slot.leaseOwner!,
      fencingToken: claimed.slot.fencingToken,
    };
    assert.equal(
      await repository.completeGeneration(previous, {
        branchOid: "e".repeat(40),
        outcome: "abandoned",
      }),
      true,
    );
    assert.equal(await repository.retain(previous, "e".repeat(40)), true);
    const reclaimed = await repository.claim(claimInput());
    assert.equal(reclaimed.status, "claimed");
    if (reclaimed.status !== "claimed") return;
    const advanced = await repository.advanceAll([
      {
        previous: {
          ...previous,
          fencingToken: reclaimed.slot.fencingToken,
        },
        next: claimInput({
          owningRunId: "run-2",
          leaseOwner: "run-2:reviewer",
          branchName: "conduit/run-2/reviewer",
          assignmentHash: "f".repeat(64),
        }),
      },
    ]);
    assert.equal(advanced[0]?.generation, 2);
    assert.equal(advanced[0]?.owningRunId, "run-2");
    assert.equal(
      (await repository.listGenerations("a".repeat(64), "reviewer")).length,
      2,
    );
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("concurrent contenders produce one fenced slot owner", async () => {
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-role-race-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRoleWorkspaceRepository(connection);
    const [first, second] = await Promise.all([
      repository.claim(claimInput()),
      repository.claim(
        claimInput({
          owningRunId: "run-2",
          leaseOwner: "process-2",
          branchName: "conduit/run-2/reviewer",
        }),
      ),
    ]);
    assert.equal(
      [first, second].filter((result) => result.status === "claimed").length,
      1,
    );
    assert.equal(
      [first, second].filter((result) => result.status === "lease-conflict")
        .length,
      1,
    );
    assert.equal(
      (await repository.listGenerations("a".repeat(64), "reviewer")).length,
      1,
    );
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("batch claims roll back every slot when one role conflicts", async () => {
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-role-batch-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRoleWorkspaceRepository(connection);
    assert.equal((await repository.claim(claimInput())).status, "claimed");
    const batch = await repository.claimAll([
      claimInput({
        roleKey: "qa",
        workspacePath: "/tmp/conduit-slots/qa",
        branchName: "conduit/run-2/qa",
        owningRunId: "run-2",
        leaseOwner: "process-2",
      }),
      claimInput({
        owningRunId: "run-2",
        leaseOwner: "process-2",
        branchName: "conduit/run-2/reviewer",
      }),
    ]);
    assert.equal(batch.status, "lease-conflict");
    assert.equal(await repository.load("a".repeat(64), "qa"), undefined);
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("workspace-path and branch collisions return bounded conflicts", async () => {
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-role-collision-"),
  );
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRoleWorkspaceRepository(connection);
    assert.equal((await repository.claim(claimInput())).status, "claimed");
    const pathCollision = await repository.claim(
      claimInput({
        roleKey: "qa",
        owningRunId: "run-2",
        leaseOwner: "process-2",
        branchName: "conduit/run-2/qa",
      }),
    );
    assert.equal(pathCollision.status, "lease-conflict");
    const branchCollision = await repository.claim(
      claimInput({
        roleKey: "documentation",
        workspacePath: "/tmp/conduit-slots/documentation",
        owningRunId: "run-3",
        leaseOwner: "process-3",
      }),
    );
    assert.equal(branchCollision.status, "lease-conflict");
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("database constraints reject invalid generations and fencing tokens", async () => {
  const projectRoot = await mkdtemp(
    path.join(tmpdir(), "conduit-role-checks-"),
  );
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const database = createTursoKysely<RunsDatabase>(connection);
    const values: RunsDatabase["role_workspace_slots"] = {
      repository_id: "a".repeat(64),
      role_key: "reviewer",
      generation: 0,
      workspace_path: "/tmp/conduit-slots/reviewer",
      owning_run_id: "run-1",
      state: RoleWorkspaceState.Provisioning,
      starting_head: "b".repeat(40),
      package_hash: "c".repeat(64),
      assignment_hash: "d".repeat(64),
      worktree_head: null,
      branch_name: "conduit/run-1/reviewer",
      lease_owner: "process-1",
      fencing_token: -1,
      leased_at: new Date().toISOString(),
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    await assert.rejects(() =>
      database.insertInto("role_workspace_slots").values(values).execute(),
    );
    await database.destroy();
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("a role slot cannot be overwritten by another run", async () => {
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-role-owner-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoRoleWorkspaceRepository(connection);
    const claimed = await repository.claim(claimInput());
    assert.equal(claimed.status, "claimed");
    if (claimed.status !== "claimed") return;
    assert.equal(
      await repository.retain(
        {
          repositoryId: claimed.slot.repositoryId,
          roleKey: claimed.slot.roleKey,
          owningRunId: claimed.slot.owningRunId,
          leaseOwner: claimed.slot.leaseOwner!,
          fencingToken: claimed.slot.fencingToken,
        },
        "e".repeat(40),
      ),
      true,
    );
    const conflict = await repository.claim(
      claimInput({
        owningRunId: "run-2",
        leaseOwner: "process-2",
        branchName: "conduit/run-2/reviewer",
      }),
    );
    assert.equal(conflict.status, "identity-conflict");
    assert.equal(conflict.owningRunId, "run-1");
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
