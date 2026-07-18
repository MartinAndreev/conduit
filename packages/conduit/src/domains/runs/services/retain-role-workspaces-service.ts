import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import type { Run } from "../types/run.js";

export async function retainClaimedRoleWorkspaces(
  run: Run,
  repository: RoleWorkspaceRepository,
): Promise<readonly string[]> {
  const failures: string[] = [];
  for (const role of run.roles) {
    if (
      !role.workspaceRepositoryId ||
      !role.workspaceRoleKey ||
      !role.workspaceLeaseOwner ||
      role.workspaceFencingToken === undefined
    )
      continue;
    const slot = await repository.load(
      role.workspaceRepositoryId,
      role.workspaceRoleKey,
    );
    if (
      !slot ||
      slot.owningRunId !== run.id ||
      slot.leaseOwner !== role.workspaceLeaseOwner ||
      slot.fencingToken !== role.workspaceFencingToken
    )
      continue;
    const retained = await repository.retain(
      {
        repositoryId: role.workspaceRepositoryId,
        roleKey: role.workspaceRoleKey,
        owningRunId: run.id,
        leaseOwner: role.workspaceLeaseOwner,
        fencingToken: role.workspaceFencingToken,
      },
      role.worktreeHead ?? slot.worktreeHead ?? run.startingHead ?? "",
    );
    if (!retained) failures.push(role.name);
  }
  return failures;
}
