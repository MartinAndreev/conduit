import type { RoleWorkspaceState } from "../enums/role-workspace-state.js";

export interface RepositoryIdentity {
  readonly repositoryId: string;
  readonly commonDirectory: string;
}

export interface RoleWorkspaceSlot {
  readonly repositoryId: string;
  readonly roleKey: string;
  readonly generation: number;
  readonly workspacePath: string;
  readonly owningRunId: string;
  readonly state: RoleWorkspaceState;
  readonly startingHead: string;
  readonly packageHash: string;
  readonly assignmentHash: string;
  readonly worktreeHead?: string;
  readonly branchName: string;
  readonly leaseOwner?: string;
  readonly fencingToken: number;
  readonly leasedAt?: string;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface RoleWorkspaceGeneration {
  readonly repositoryId: string;
  readonly roleKey: string;
  readonly generation: number;
  readonly workspacePath: string;
  readonly owningRunId: string;
  readonly startingHead: string;
  readonly packageHash: string;
  readonly assignmentHash: string;
  readonly branchName: string;
  readonly branchOid?: string;
  readonly outcome?: string;
  readonly promotionOid?: string;
  readonly createdAt: string;
  readonly completedAt?: string;
}

export interface RoleWorkspaceClaimInput {
  readonly repositoryId: string;
  readonly roleKey: string;
  readonly workspacePath: string;
  readonly owningRunId: string;
  readonly startingHead: string;
  readonly packageHash: string;
  readonly assignmentHash: string;
  readonly branchName: string;
  readonly leaseOwner: string;
}

export interface RoleWorkspaceLeaseIdentity {
  readonly repositoryId: string;
  readonly roleKey: string;
  readonly owningRunId: string;
  readonly leaseOwner: string;
  readonly fencingToken: number;
}

export interface RoleWorkspaceAdvanceInput {
  readonly previous: RoleWorkspaceLeaseIdentity;
  readonly next: RoleWorkspaceClaimInput;
}

export interface RoleWorkspaceGenerationCompletion {
  readonly branchOid: string;
  readonly outcome: string;
  readonly promotionOid?: string;
}

export type RoleWorkspaceClaimBatchResult =
  | Readonly<{ status: "claimed"; slots: readonly RoleWorkspaceSlot[] }>
  | Readonly<{
      status: "lease-conflict" | "identity-conflict";
      owningRunId: string;
      leaseOwner?: string;
    }>;

export type RoleWorkspaceClaimResult =
  | Readonly<{ status: "claimed"; slot: RoleWorkspaceSlot }>
  | Readonly<{
      status: "lease-conflict" | "identity-conflict";
      owningRunId: string;
      leaseOwner?: string;
    }>;
