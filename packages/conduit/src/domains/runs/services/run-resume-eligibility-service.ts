import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";
import { hashFeaturePackage } from "../../features/services/feature-package-hasher.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import { RoleWorkspaceState } from "../enums/role-workspace-state.js";
import { resolveRepositoryIdentity } from "./role-workspace-identity-service.js";
import type { ResumeEligibility } from "../types/resume-eligibility.js";
import type { Run, RunRole } from "../types/run.js";
import { gitWorktreeRegistry } from "@system/git/services/git-worktree-registry-service.js";

function notResumable(reason: string): ResumeEligibility {
  return {
    state: "not-resumable",
    reason,
    preservedRoles: [],
    retryRoles: [],
    reconstructRoles: [],
  };
}

function gitOutput(root: string, args: readonly string[]): string | undefined {
  const result = spawnSync("git", ["-C", root, ...args], { encoding: "utf8" });
  return result.status === 0 ? result.stdout.trim() : undefined;
}

function reusableWorktree(
  role: RunRole,
  allowFailedTurnChanges = false,
): boolean {
  if (!role.worktree || !role.worktreeHead) return false;
  const expected = path.resolve(role.worktree);
  if (
    path.resolve(
      gitOutput(expected, ["rev-parse", "--show-toplevel"]) ?? "",
    ) !== expected
  )
    return false;
  if (gitOutput(expected, ["rev-parse", "HEAD"]) !== role.worktreeHead)
    return false;
  if (
    role.workspaceRepositoryId &&
    resolveRepositoryIdentity(expected).repositoryId !==
      role.workspaceRepositoryId
  )
    return false;
  if (
    role.workspaceBranchName &&
    gitOutput(expected, ["symbolic-ref", "--quiet", "--short", "HEAD"]) !==
      role.workspaceBranchName
  )
    return false;
  const status = gitOutput(expected, [
    "status",
    "--porcelain",
    "--untracked-files=all",
  ]);
  return status !== undefined && (status === "" || allowFailedTurnChanges);
}

function commitIsValidDescendant(
  projectRoot: string,
  startingHead: string,
  commit: string,
): boolean {
  if (
    gitOutput(projectRoot, ["cat-file", "-e", `${commit}^{commit}`]) ===
    undefined
  )
    return false;
  const result = spawnSync(
    "git",
    ["-C", projectRoot, "merge-base", "--is-ancestor", startingHead, commit],
    { encoding: "utf8" },
  );
  return result.status === 0;
}

export async function evaluateRunResumeEligibility(input: {
  readonly projectRoot: string;
  readonly run: Run;
  readonly resultRepository?: ConduitResultRecordRepository;
  readonly roleWorkspaceRepository?: RoleWorkspaceRepository;
}): Promise<ResumeEligibility> {
  const { projectRoot, run, resultRepository, roleWorkspaceRepository } = input;
  if (run.status !== "failed")
    return notResumable(`Run is ${run.status}, not failed.`);
  if (!run.startingHead || !run.featurePackageHash || !run.featurePackagePath)
    return notResumable("Run predates verifiable resume identity metadata.");
  if (gitOutput(projectRoot, ["rev-parse", "HEAD"]) !== run.startingHead)
    return notResumable("Project revision changed since this run started.");

  try {
    const packageRoot = path.resolve(projectRoot, run.featurePackagePath);
    const relativePackage = path.relative(projectRoot, packageRoot);
    if (
      relativePackage === ".." ||
      relativePackage.startsWith(`..${path.sep}`) ||
      path.isAbsolute(relativePackage)
    )
      return notResumable("Feature package path is outside the project.");
    const currentPackage = await hashFeaturePackage({
      packageRoot,
      ownershipInputs: run.roles.map((role) => ({
        role: role.name,
        readOnly: role.readOnly,
        owns: role.owns,
        dependsOn: role.dependsOn,
      })),
    });
    if (currentPackage.hash !== run.featurePackageHash)
      return notResumable("Feature package changed since this run started.");
  } catch {
    return notResumable("Feature package identity could not be verified.");
  }

  const preservedRoles: string[] = [];
  const retryRoles: string[] = [];
  const reconstructRoles: string[] = [];
  let approvedReviewerPreserved = false;
  for (const role of run.roles) {
    let provisioningReconstructable = false;
    if (roleWorkspaceRepository) {
      if (
        !role.workspaceRepositoryId ||
        !role.workspaceRoleKey ||
        !role.workspaceBranchName ||
        !role.workspaceAssignmentHash ||
        !role.workspaceLeaseOwner ||
        !role.worktree
      )
        return notResumable(
          `Role ${role.name} predates canonical workspace metadata.`,
        );
      const slot = await roleWorkspaceRepository.load(
        role.workspaceRepositoryId,
        role.workspaceRoleKey,
      );
      if (
        !slot ||
        slot.owningRunId !== run.id ||
        path.resolve(slot.workspacePath) !== path.resolve(role.worktree) ||
        slot.branchName !== role.workspaceBranchName ||
        slot.startingHead !== run.startingHead ||
        slot.packageHash !== run.featurePackageHash ||
        slot.assignmentHash !== role.workspaceAssignmentHash
      )
        return notResumable(
          `Role ${role.name} canonical workspace slot diverged.`,
        );
      if (role.worktreeHead) {
        if (slot.worktreeHead !== role.worktreeHead)
          return notResumable(
            `Role ${role.name} workspace HEAD is not canonical.`,
          );
      } else {
        const unstartedRetainedSlot =
          role.status !== "completed" &&
          slot.state === RoleWorkspaceState.Retained &&
          slot.worktreeHead === run.startingHead;
        const provisioningSlot =
          slot.state === RoleWorkspaceState.Provisioning && !slot.worktreeHead;
        if (!provisioningSlot && !unstartedRetainedSlot)
          return notResumable(
            `Role ${role.name} has no canonical workspace HEAD.`,
          );
        const expected = path.resolve(role.worktree);
        if (existsSync(expected)) {
          if (unstartedRetainedSlot)
            return notResumable(
              `Role ${role.name} unstarted workspace path unexpectedly exists.`,
            );
          const observedHead = gitOutput(expected, ["rev-parse", "HEAD"]);
          if (
            !observedHead ||
            !reusableWorktree({ ...role, worktreeHead: observedHead })
          )
            return notResumable(
              `Role ${role.name} provisioning workspace cannot be proven.`,
            );
        } else {
          if (
            gitOutput(projectRoot, [
              "rev-parse",
              "--verify",
              `refs/heads/${role.workspaceBranchName}`,
            ]) !== undefined
          )
            return notResumable(
              `Role ${role.name} branch exists without its registered workspace.`,
            );
          if (gitWorktreeRegistry.find(projectRoot, expected))
            return notResumable(
              `Role ${role.name} missing workspace remains registered by Git.`,
            );
        }
        provisioningReconstructable = true;
      }
    }
    if (role.status === "completed") {
      if (!resultRepository)
        return notResumable(
          "Completed role results are unavailable for verification.",
        );
      const result = await resultRepository.load(run.id, role.name);
      if (
        !result ||
        !result.process.acceptable ||
        !result.protocolValidation.valid ||
        !result.semanticValidation.valid ||
        result.response?.status !== "completed"
      )
        return notResumable(
          `Completed role ${role.name} has no valid result record.`,
        );
      if (
        !(role.integrationCommits ?? []).every((commit) =>
          commitIsValidDescendant(projectRoot, run.startingHead!, commit),
        )
      )
        return notResumable(
          `Completed role ${role.name} has unavailable integration commits.`,
        );
      if (
        role.name === "reviewer" &&
        result.response?.verdict?.decision !== "approved"
      ) {
        retryRoles.push(role.name);
        continue;
      }
      preservedRoles.push(role.name);
      if (role.name === "reviewer") approvedReviewerPreserved = true;
      continue;
    }
    if (provisioningReconstructable) {
      reconstructRoles.push(role.name);
    } else if (role.worktree || role.worktreeHead) {
      const failedTurnCanBeCheckpointed =
        role.lastFailureKind === "missing-response" ||
        role.lastFailureKind === "structural-response" ||
        role.lastFailureKind === "semantic-response";
      if (!reusableWorktree(role, failedTurnCanBeCheckpointed))
        return notResumable(
          `Role ${role.name} no longer has a verified reusable workspace.`,
        );
    } else {
      reconstructRoles.push(role.name);
    }
    retryRoles.push(role.name);
  }
  if (!retryRoles.length && !approvedReviewerPreserved)
    return notResumable("No failed or unfinished roles remain.");
  return {
    state: "resumable",
    preservedRoles,
    retryRoles,
    reconstructRoles,
  };
}
