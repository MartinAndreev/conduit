import type { Transaction } from "kysely";
import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { RoleWorkspaceState } from "../enums/role-workspace-state.js";
import type { RunsDatabase } from "../interfaces/database-schema.js";
import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import type {
  RoleWorkspaceAdvanceInput,
  RoleWorkspaceClaimBatchResult,
  RoleWorkspaceClaimInput,
  RoleWorkspaceClaimResult,
  RoleWorkspaceGeneration,
  RoleWorkspaceGenerationCompletion,
  RoleWorkspaceLeaseIdentity,
  RoleWorkspaceSlot,
} from "../types/role-workspace.js";

function slotFromRow(
  row: RunsDatabase["role_workspace_slots"],
): RoleWorkspaceSlot {
  return {
    repositoryId: row.repository_id,
    roleKey: row.role_key,
    generation: row.generation,
    workspacePath: row.workspace_path,
    owningRunId: row.owning_run_id,
    state: row.state as RoleWorkspaceState,
    startingHead: row.starting_head,
    packageHash: row.package_hash,
    assignmentHash: row.assignment_hash,
    ...(row.worktree_head ? { worktreeHead: row.worktree_head } : {}),
    branchName: row.branch_name,
    ...(row.lease_owner ? { leaseOwner: row.lease_owner } : {}),
    fencingToken: row.fencing_token,
    ...(row.leased_at ? { leasedAt: row.leased_at } : {}),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function generationFromRow(
  row: RunsDatabase["role_workspace_generations"],
): RoleWorkspaceGeneration {
  return {
    repositoryId: row.repository_id,
    roleKey: row.role_key,
    generation: row.generation,
    workspacePath: row.workspace_path,
    owningRunId: row.owning_run_id,
    startingHead: row.starting_head,
    packageHash: row.package_hash,
    assignmentHash: row.assignment_hash,
    branchName: row.branch_name,
    ...(row.branch_oid ? { branchOid: row.branch_oid } : {}),
    ...(row.outcome ? { outcome: row.outcome } : {}),
    ...(row.promotion_oid ? { promotionOid: row.promotion_oid } : {}),
    createdAt: row.created_at,
    ...(row.completed_at ? { completedAt: row.completed_at } : {}),
  };
}

function identityMatches(
  slot: RoleWorkspaceSlot,
  input: RoleWorkspaceClaimInput,
): boolean {
  return (
    slot.owningRunId === input.owningRunId &&
    slot.workspacePath === input.workspacePath &&
    slot.startingHead === input.startingHead &&
    slot.packageHash === input.packageHash &&
    slot.assignmentHash === input.assignmentHash &&
    slot.branchName === input.branchName
  );
}

async function claimInTransaction(
  transaction: Transaction<RunsDatabase>,
  input: RoleWorkspaceClaimInput,
): Promise<RoleWorkspaceClaimResult> {
  const now = new Date().toISOString();
  const collision = await transaction
    .selectFrom("role_workspace_slots")
    .selectAll()
    .where((expression) =>
      expression.or([
        expression.and([
          expression("repository_id", "=", input.repositoryId),
          expression("role_key", "=", input.roleKey),
        ]),
        expression("workspace_path", "=", input.workspacePath),
        expression("branch_name", "=", input.branchName),
      ]),
    )
    .executeTakeFirst();
  if (!collision) {
    await transaction
      .insertInto("role_workspace_slots")
      .values({
        repository_id: input.repositoryId,
        role_key: input.roleKey,
        generation: 1,
        workspace_path: input.workspacePath,
        owning_run_id: input.owningRunId,
        state: RoleWorkspaceState.Provisioning,
        starting_head: input.startingHead,
        package_hash: input.packageHash,
        assignment_hash: input.assignmentHash,
        worktree_head: null,
        branch_name: input.branchName,
        lease_owner: input.leaseOwner,
        fencing_token: 1,
        leased_at: now,
        created_at: now,
        updated_at: now,
      })
      .execute();
    await transaction
      .insertInto("role_workspace_generations")
      .values({
        repository_id: input.repositoryId,
        role_key: input.roleKey,
        generation: 1,
        workspace_path: input.workspacePath,
        owning_run_id: input.owningRunId,
        starting_head: input.startingHead,
        package_hash: input.packageHash,
        assignment_hash: input.assignmentHash,
        branch_name: input.branchName,
        branch_oid: null,
        outcome: null,
        promotion_oid: null,
        created_at: now,
        completed_at: null,
      })
      .execute();
    const created = await transaction
      .selectFrom("role_workspace_slots")
      .selectAll()
      .where("repository_id", "=", input.repositoryId)
      .where("role_key", "=", input.roleKey)
      .executeTakeFirstOrThrow();
    return { status: "claimed", slot: slotFromRow(created) };
  }

  const existing = slotFromRow(collision);
  if (existing.leaseOwner && existing.leaseOwner !== input.leaseOwner)
    return {
      status: "lease-conflict",
      owningRunId: existing.owningRunId,
      leaseOwner: existing.leaseOwner,
    };
  if (!identityMatches(existing, input))
    return {
      status: "identity-conflict",
      owningRunId: existing.owningRunId,
      ...(existing.leaseOwner ? { leaseOwner: existing.leaseOwner } : {}),
    };
  if (existing.leaseOwner === input.leaseOwner)
    return { status: "claimed", slot: existing };

  const fencingToken = existing.fencingToken + 1;
  const claimed = await transaction
    .updateTable("role_workspace_slots")
    .set({
      lease_owner: input.leaseOwner,
      fencing_token: fencingToken,
      leased_at: now,
      updated_at: now,
    })
    .where("repository_id", "=", input.repositoryId)
    .where("role_key", "=", input.roleKey)
    .where("owning_run_id", "=", input.owningRunId)
    .where("lease_owner", "is", null)
    .where("fencing_token", "=", existing.fencingToken)
    .executeTakeFirst();
  if (claimed.numUpdatedRows !== 1n)
    return {
      status: "lease-conflict",
      owningRunId: existing.owningRunId,
    };
  const updated = await transaction
    .selectFrom("role_workspace_slots")
    .selectAll()
    .where("repository_id", "=", input.repositoryId)
    .where("role_key", "=", input.roleKey)
    .executeTakeFirstOrThrow();
  return { status: "claimed", slot: slotFromRow(updated) };
}

class RoleWorkspaceBatchConflict extends Error {
  constructor(
    readonly result: Exclude<
      RoleWorkspaceClaimBatchResult,
      { status: "claimed" }
    >,
  ) {
    super(result.status);
  }
}

export class TursoRoleWorkspaceRepository implements RoleWorkspaceRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RunsDatabase>(connection);
  }

  async load(
    repositoryId: string,
    roleKey: string,
  ): Promise<RoleWorkspaceSlot | undefined> {
    const row = await this.database
      .selectFrom("role_workspace_slots")
      .selectAll()
      .where("repository_id", "=", repositoryId)
      .where("role_key", "=", roleKey)
      .executeTakeFirst();
    return row ? slotFromRow(row) : undefined;
  }

  async claim(
    input: RoleWorkspaceClaimInput,
  ): Promise<RoleWorkspaceClaimResult> {
    return this.database
      .transaction()
      .execute((transaction) => claimInTransaction(transaction, input));
  }

  async claimAll(
    inputs: readonly RoleWorkspaceClaimInput[],
  ): Promise<RoleWorkspaceClaimBatchResult> {
    try {
      return await this.database.transaction().execute(async (transaction) => {
        const slots: RoleWorkspaceSlot[] = [];
        for (const input of inputs) {
          const result = await claimInTransaction(transaction, input);
          if (result.status !== "claimed")
            throw new RoleWorkspaceBatchConflict(result);
          slots.push(result.slot);
        }
        return { status: "claimed" as const, slots };
      });
    } catch (cause) {
      if (cause instanceof RoleWorkspaceBatchConflict) return cause.result;
      throw cause;
    }
  }

  async advanceAll(
    inputs: readonly RoleWorkspaceAdvanceInput[],
  ): Promise<readonly RoleWorkspaceSlot[]> {
    return this.database.transaction().execute(async (transaction) => {
      const advanced: RoleWorkspaceSlot[] = [];
      const now = new Date().toISOString();
      for (const input of inputs) {
        const current = await transaction
          .selectFrom("role_workspace_slots")
          .selectAll()
          .where("repository_id", "=", input.previous.repositoryId)
          .where("role_key", "=", input.previous.roleKey)
          .where("owning_run_id", "=", input.previous.owningRunId)
          .where("lease_owner", "=", input.previous.leaseOwner)
          .where("fencing_token", "=", input.previous.fencingToken)
          .where("state", "=", RoleWorkspaceState.Retained)
          .executeTakeFirst();
        if (!current)
          throw new Error("Retained role workspace generation changed.");
        const completed = await transaction
          .selectFrom("role_workspace_generations")
          .select(["outcome", "completed_at"])
          .where("repository_id", "=", current.repository_id)
          .where("role_key", "=", current.role_key)
          .where("generation", "=", current.generation)
          .executeTakeFirst();
        if (!completed?.completed_at || completed.outcome !== "abandoned")
          throw new Error("Previous workspace generation was not preserved.");
        const generation = current.generation + 1;
        await transaction
          .insertInto("role_workspace_generations")
          .values({
            repository_id: input.next.repositoryId,
            role_key: input.next.roleKey,
            generation,
            workspace_path: input.next.workspacePath,
            owning_run_id: input.next.owningRunId,
            starting_head: input.next.startingHead,
            package_hash: input.next.packageHash,
            assignment_hash: input.next.assignmentHash,
            branch_name: input.next.branchName,
            branch_oid: null,
            outcome: null,
            promotion_oid: null,
            created_at: now,
            completed_at: null,
          })
          .execute();
        const result = await transaction
          .updateTable("role_workspace_slots")
          .set({
            generation,
            workspace_path: input.next.workspacePath,
            owning_run_id: input.next.owningRunId,
            state: RoleWorkspaceState.Provisioning,
            starting_head: input.next.startingHead,
            package_hash: input.next.packageHash,
            assignment_hash: input.next.assignmentHash,
            worktree_head: null,
            branch_name: input.next.branchName,
            lease_owner: input.next.leaseOwner,
            fencing_token: current.fencing_token + 1,
            leased_at: now,
            updated_at: now,
          })
          .where("repository_id", "=", current.repository_id)
          .where("role_key", "=", current.role_key)
          .where("generation", "=", current.generation)
          .where("fencing_token", "=", current.fencing_token)
          .executeTakeFirst();
        if (result.numUpdatedRows !== 1n)
          throw new Error("Role workspace generation advance conflicted.");
        const row = await transaction
          .selectFrom("role_workspace_slots")
          .selectAll()
          .where("repository_id", "=", current.repository_id)
          .where("role_key", "=", current.role_key)
          .executeTakeFirstOrThrow();
        advanced.push(slotFromRow(row));
      }
      return advanced;
    });
  }

  async recordHead(
    identity: RoleWorkspaceLeaseIdentity,
    worktreeHead: string,
  ): Promise<boolean> {
    const result = await this.database
      .updateTable("role_workspace_slots")
      .set({
        worktree_head: worktreeHead,
        updated_at: new Date().toISOString(),
      })
      .where("repository_id", "=", identity.repositoryId)
      .where("role_key", "=", identity.roleKey)
      .where("owning_run_id", "=", identity.owningRunId)
      .where("lease_owner", "=", identity.leaseOwner)
      .where("fencing_token", "=", identity.fencingToken)
      .executeTakeFirst();
    return result.numUpdatedRows === 1n;
  }

  async retain(
    identity: RoleWorkspaceLeaseIdentity,
    worktreeHead: string,
  ): Promise<boolean> {
    const result = await this.database
      .updateTable("role_workspace_slots")
      .set({
        state: RoleWorkspaceState.Retained,
        worktree_head: worktreeHead,
        lease_owner: null,
        leased_at: null,
        updated_at: new Date().toISOString(),
      })
      .where("repository_id", "=", identity.repositoryId)
      .where("role_key", "=", identity.roleKey)
      .where("owning_run_id", "=", identity.owningRunId)
      .where("lease_owner", "=", identity.leaseOwner)
      .where("fencing_token", "=", identity.fencingToken)
      .executeTakeFirst();
    return result.numUpdatedRows === 1n;
  }

  async transition(
    identity: RoleWorkspaceLeaseIdentity,
    expectedState: RoleWorkspaceState,
    nextState: RoleWorkspaceState,
  ): Promise<boolean> {
    const result = await this.database
      .updateTable("role_workspace_slots")
      .set({ state: nextState, updated_at: new Date().toISOString() })
      .where("repository_id", "=", identity.repositoryId)
      .where("role_key", "=", identity.roleKey)
      .where("owning_run_id", "=", identity.owningRunId)
      .where("lease_owner", "=", identity.leaseOwner)
      .where("fencing_token", "=", identity.fencingToken)
      .where("state", "=", expectedState)
      .executeTakeFirst();
    return result.numUpdatedRows === 1n;
  }

  async completeGeneration(
    identity: RoleWorkspaceLeaseIdentity,
    completion: RoleWorkspaceGenerationCompletion,
  ): Promise<boolean> {
    return this.database.transaction().execute(async (transaction) => {
      const slot = await transaction
        .selectFrom("role_workspace_slots")
        .select(["generation"])
        .where("repository_id", "=", identity.repositoryId)
        .where("role_key", "=", identity.roleKey)
        .where("owning_run_id", "=", identity.owningRunId)
        .where("lease_owner", "=", identity.leaseOwner)
        .where("fencing_token", "=", identity.fencingToken)
        .executeTakeFirst();
      if (!slot) return false;
      const existing = await transaction
        .selectFrom("role_workspace_generations")
        .select(["branch_oid", "outcome", "promotion_oid", "completed_at"])
        .where("repository_id", "=", identity.repositoryId)
        .where("role_key", "=", identity.roleKey)
        .where("generation", "=", slot.generation)
        .executeTakeFirst();
      if (!existing) return false;
      if (existing.completed_at)
        return (
          existing.branch_oid === completion.branchOid &&
          existing.outcome === completion.outcome &&
          existing.promotion_oid === (completion.promotionOid ?? null)
        );
      const result = await transaction
        .updateTable("role_workspace_generations")
        .set({
          branch_oid: completion.branchOid,
          outcome: completion.outcome,
          promotion_oid: completion.promotionOid ?? null,
          completed_at: new Date().toISOString(),
        })
        .where("repository_id", "=", identity.repositoryId)
        .where("role_key", "=", identity.roleKey)
        .where("generation", "=", slot.generation)
        .where("completed_at", "is", null)
        .executeTakeFirst();
      return result.numUpdatedRows === 1n;
    });
  }

  async remove(
    identity: RoleWorkspaceLeaseIdentity,
    expectedState: RoleWorkspaceState,
  ): Promise<boolean> {
    const result = await this.database
      .deleteFrom("role_workspace_slots")
      .where("repository_id", "=", identity.repositoryId)
      .where("role_key", "=", identity.roleKey)
      .where("owning_run_id", "=", identity.owningRunId)
      .where("lease_owner", "=", identity.leaseOwner)
      .where("fencing_token", "=", identity.fencingToken)
      .where("state", "=", expectedState)
      .executeTakeFirst();
    return result.numDeletedRows === 1n;
  }

  async listByRun(runId: string): Promise<readonly RoleWorkspaceSlot[]> {
    const rows = await this.database
      .selectFrom("role_workspace_slots")
      .selectAll()
      .where("owning_run_id", "=", runId)
      .orderBy("role_key")
      .execute();
    return rows.map(slotFromRow);
  }

  async listCleanupCandidates(
    repositoryId: string,
  ): Promise<readonly RoleWorkspaceSlot[]> {
    const stateRows = await this.database
      .selectFrom("role_workspace_slots")
      .selectAll()
      .where("repository_id", "=", repositoryId)
      .where("state", "in", [
        RoleWorkspaceState.PromotionPending,
        RoleWorkspaceState.Promoted,
        RoleWorkspaceState.CleanupPending,
      ])
      .execute();
    const completedGenerations = await this.database
      .selectFrom("role_workspace_generations")
      .select(["role_key", "generation"])
      .where("repository_id", "=", repositoryId)
      .where("outcome", "=", "promoted")
      .where("promotion_oid", "is not", null)
      .where("completed_at", "is not", null)
      .execute();
    const rows = new Map<string, (typeof stateRows)[number]>(
      stateRows.map((row) => [`${row.role_key}:${row.generation}`, row]),
    );
    for (const generation of completedGenerations) {
      const key = `${generation.role_key}:${generation.generation}`;
      if (rows.has(key)) continue;
      const slot = await this.database
        .selectFrom("role_workspace_slots")
        .selectAll()
        .where("repository_id", "=", repositoryId)
        .where("role_key", "=", generation.role_key)
        .where("generation", "=", generation.generation)
        .executeTakeFirst();
      if (slot) rows.set(key, slot);
    }
    return [...rows.values()]
      .sort((left, right) => left.role_key.localeCompare(right.role_key))
      .map(slotFromRow);
  }

  async listGenerations(
    repositoryId: string,
    roleKey: string,
  ): Promise<readonly RoleWorkspaceGeneration[]> {
    const rows = await this.database
      .selectFrom("role_workspace_generations")
      .selectAll()
      .where("repository_id", "=", repositoryId)
      .where("role_key", "=", roleKey)
      .orderBy("generation")
      .execute();
    return rows.map(generationFromRow);
  }
}
