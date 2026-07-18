import type { RoleWorkspaceState } from "../enums/role-workspace-state.js";
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

export interface RoleWorkspaceRepository {
  load(
    repositoryId: string,
    roleKey: string,
  ): Promise<RoleWorkspaceSlot | undefined>;
  claim(input: RoleWorkspaceClaimInput): Promise<RoleWorkspaceClaimResult>;
  claimAll(
    inputs: readonly RoleWorkspaceClaimInput[],
  ): Promise<RoleWorkspaceClaimBatchResult>;
  advanceAll(
    inputs: readonly RoleWorkspaceAdvanceInput[],
  ): Promise<readonly RoleWorkspaceSlot[]>;
  recordHead(
    identity: RoleWorkspaceLeaseIdentity,
    worktreeHead: string,
  ): Promise<boolean>;
  retain(
    identity: RoleWorkspaceLeaseIdentity,
    worktreeHead: string,
  ): Promise<boolean>;
  transition(
    identity: RoleWorkspaceLeaseIdentity,
    expectedState: RoleWorkspaceState,
    nextState: RoleWorkspaceState,
  ): Promise<boolean>;
  completeGeneration(
    identity: RoleWorkspaceLeaseIdentity,
    completion: RoleWorkspaceGenerationCompletion,
  ): Promise<boolean>;
  remove(
    identity: RoleWorkspaceLeaseIdentity,
    expectedState: RoleWorkspaceState,
  ): Promise<boolean>;
  listByRun(runId: string): Promise<readonly RoleWorkspaceSlot[]>;
  listCleanupCandidates(
    repositoryId: string,
  ): Promise<readonly RoleWorkspaceSlot[]>;
  listGenerations(
    repositoryId: string,
    roleKey: string,
  ): Promise<readonly RoleWorkspaceGeneration[]>;
}
