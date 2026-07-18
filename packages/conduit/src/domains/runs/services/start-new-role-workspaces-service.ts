import { existsSync, lstatSync, rmSync } from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import type { Run, RunRole } from "../types/run.js";
import type {
  RoleWorkspaceAdvanceInput,
  RoleWorkspaceLeaseIdentity,
  RoleWorkspaceSlot,
} from "../types/role-workspace.js";
import { resolveRepositoryIdentity } from "./role-workspace-identity-service.js";
import { gitWorktreeRegistry } from "@system/git/services/git-worktree-registry-service.js";

const inProgressGitPaths = [
  "MERGE_HEAD",
  "CHERRY_PICK_HEAD",
  "REVERT_HEAD",
  "REBASE_HEAD",
  "BISECT_LOG",
  "BISECT_START",
  "AM_HEAD",
  "rebase-apply",
  "rebase-merge",
  "sequencer",
] as const;

function git(root: string, args: readonly string[]): string | undefined {
  const result = spawnSync("git", ["-C", root, ...args], { encoding: "utf8" });
  return result.status === 0 ? result.stdout.trim() : undefined;
}

function verifiedRecordedLinks(role: RunRole): readonly string[] {
  if (!role.worktree) return [];
  const root = path.resolve(role.worktree);
  return (role.linkedWorkspacePaths ?? []).map((relativePath) => {
    const target = path.resolve(root, relativePath);
    const relative = path.relative(root, target);
    if (
      relative === ".." ||
      relative.startsWith(`..${path.sep}`) ||
      path.isAbsolute(relative)
    )
      throw new Error(`Recorded workspace link escaped role ${role.name}.`);
    if (!existsSync(target))
      throw new Error(
        `Recorded workspace dependency ${relativePath} is missing.`,
      );
    const stat = lstatSync(target);
    if (!stat.isSymbolicLink() && !stat.isDirectory())
      throw new Error(
        `Recorded workspace dependency ${relativePath} has an invalid type.`,
      );
    return relative.replaceAll(path.sep, "/");
  });
}

function assertNoGitOperation(workspacePath: string, roleName: string): void {
  for (const gitPath of inProgressGitPaths) {
    const resolved = git(workspacePath, ["rev-parse", "--git-path", gitPath]);
    if (!resolved)
      throw new Error(`Role ${roleName} Git operation state is unavailable.`);
    if (existsSync(path.resolve(workspacePath, resolved)))
      throw new Error(`Role ${roleName} has an in-progress Git operation.`);
  }
}

function assertCleanExceptLinks(
  workspacePath: string,
  roleName: string,
  links: readonly string[],
): void {
  const status = git(workspacePath, [
    "status",
    "--porcelain",
    "--untracked-files=all",
    "--ignored=matching",
  ]);
  if (status === undefined)
    throw new Error(`Role ${roleName} workspace status is unavailable.`);
  const unexpected = status
    .split("\n")
    .filter(Boolean)
    .filter((line) => {
      const entry = line.slice(3).replace(/\/$/, "");
      return !links.some(
        (link) => entry === link || entry.startsWith(`${link}/`),
      );
    });
  if (unexpected.length)
    throw new Error(`Role ${roleName} has uncheckpointed workspace data.`);
}

async function reconcileFailedProvisioningSlot(input: {
  projectRoot: string;
  previousRun: Run;
  previousRole: RunRole;
  slot: RoleWorkspaceSlot;
  repository: RoleWorkspaceRepository;
}): Promise<RoleWorkspaceSlot> {
  const { projectRoot, previousRun, previousRole, slot, repository } = input;
  if (
    previousRun.status !== "failed" ||
    slot.state !== "provisioning" ||
    slot.worktreeHead ||
    previousRole.worktreeHead ||
    !slot.leaseOwner ||
    slot.leaseOwner !== previousRole.workspaceLeaseOwner ||
    slot.owningRunId !== previousRun.id ||
    slot.startingHead !== previousRun.startingHead
  )
    return slot;
  const workspaceExists = existsSync(slot.workspacePath);
  const registration = gitWorktreeRegistry.find(
    projectRoot,
    slot.workspacePath,
  );
  const expectedBranchOid = git(projectRoot, [
    "rev-parse",
    "--verify",
    `refs/heads/${slot.branchName}`,
  ]);
  const staleConduitRegistration =
    !workspaceExists &&
    registration?.prunable === true &&
    !registration.locked &&
    Boolean(registration.head) &&
    Boolean(registration.branch?.startsWith("conduit/")) &&
    registration.branch !== slot.branchName &&
    git(projectRoot, [
      "rev-parse",
      "--verify",
      `refs/heads/${registration.branch}`,
    ]) === registration.head;
  const entirelyUnmaterialized =
    !workspaceExists && !registration && !expectedBranchOid;
  if (!staleConduitRegistration && !entirelyUnmaterialized) return slot;
  const retained = await repository.retain(
    {
      repositoryId: slot.repositoryId,
      roleKey: slot.roleKey,
      owningRunId: slot.owningRunId,
      leaseOwner: slot.leaseOwner,
      fencingToken: slot.fencingToken,
    },
    slot.startingHead,
  );
  if (!retained)
    throw new Error(
      `Role ${previousRole.name} failed provisioning state could not be reconciled.`,
    );
  return (await repository.load(slot.repositoryId, slot.roleKey)) ?? slot;
}

async function preflightContext(input: {
  projectRoot: string;
  repositoryId: string;
  previousRun: Run;
  previousRole: RunRole;
  nextRun: Run;
  nextRole: RunRole;
  slot: RoleWorkspaceSlot;
  repository: RoleWorkspaceRepository;
}) {
  const {
    projectRoot,
    repositoryId,
    previousRun,
    previousRole,
    nextRun,
    nextRole,
    slot,
    repository,
  } = input;
  if (
    !previousRole.workspaceRepositoryId ||
    !previousRole.workspaceRoleKey ||
    !previousRole.workspaceBranchName ||
    !previousRole.workspaceAssignmentHash ||
    !previousRole.workspaceLeaseOwner ||
    !previousRole.worktree ||
    !nextRole.workspaceRepositoryId ||
    !nextRole.workspaceRoleKey ||
    !nextRole.workspaceBranchName ||
    !nextRole.workspaceAssignmentHash ||
    !nextRole.workspaceLeaseOwner ||
    !nextRole.worktree ||
    !previousRun.startingHead ||
    !previousRun.featurePackageHash ||
    !nextRun.startingHead ||
    !nextRun.featurePackageHash
  )
    throw new Error("Start Anew requires complete canonical role metadata.");
  if (
    previousRole.workspaceRepositoryId !== repositoryId ||
    nextRole.workspaceRepositoryId !== repositoryId ||
    previousRole.workspaceRoleKey !== nextRole.workspaceRoleKey ||
    path.resolve(previousRole.worktree) !== path.resolve(nextRole.worktree) ||
    path.resolve(slot.workspacePath) !== path.resolve(previousRole.worktree) ||
    slot.owningRunId !== previousRun.id ||
    slot.state !== "retained" ||
    slot.branchName !== previousRole.workspaceBranchName ||
    slot.assignmentHash !== previousRole.workspaceAssignmentHash ||
    slot.packageHash !== previousRun.featurePackageHash ||
    slot.startingHead !== previousRun.startingHead ||
    !slot.worktreeHead
  )
    throw new Error(
      `Role ${previousRole.name} retained slot is not resettable.`,
    );
  const observedBranchOid = git(projectRoot, [
    "rev-parse",
    "--verify",
    `refs/heads/${slot.branchName}`,
  ]);
  const workspaceExists = existsSync(slot.workspacePath);
  const registration = gitWorktreeRegistry.find(
    projectRoot,
    slot.workspacePath,
  );
  if (registration?.locked)
    throw new Error(
      `Role ${previousRole.name} worktree registration is locked.`,
    );
  const registeredCheckpoint = workspaceExists ? undefined : registration;
  const unstartedSlot =
    !workspaceExists &&
    !previousRole.worktreeHead &&
    slot.worktreeHead === slot.startingHead &&
    !observedBranchOid;
  const staleConduitRegistration =
    unstartedSlot &&
    registeredCheckpoint?.prunable === true &&
    Boolean(registeredCheckpoint.head) &&
    Boolean(registeredCheckpoint.branch?.startsWith("conduit/")) &&
    registeredCheckpoint.branch !== slot.branchName &&
    git(projectRoot, [
      "rev-parse",
      "--verify",
      `refs/heads/${registeredCheckpoint.branch}`,
    ]) === registeredCheckpoint.head;
  const unmaterialized =
    !workspaceExists &&
    !registeredCheckpoint &&
    !previousRole.worktreeHead &&
    slot.worktreeHead === slot.startingHead &&
    (!observedBranchOid || observedBranchOid === slot.worktreeHead);
  if (
    (observedBranchOid && observedBranchOid !== slot.worktreeHead) ||
    (!observedBranchOid && !unmaterialized && !staleConduitRegistration)
  )
    throw new Error(`Role ${previousRole.name} branch checkpoint diverged.`);
  const branchOid = observedBranchOid ?? slot.worktreeHead;
  const generations = await repository.listGenerations(
    slot.repositoryId,
    slot.roleKey,
  );
  const generation = generations.find(
    (candidate) => candidate.generation === slot.generation,
  );
  const alreadyAbandoned =
    generation?.outcome === "abandoned" &&
    generation.branchOid === branchOid &&
    Boolean(generation.completedAt);
  let links: readonly string[] = [];
  let missingRegistration = false;
  if (workspaceExists) {
    if (
      path.resolve(
        git(slot.workspacePath, ["rev-parse", "--show-toplevel"]) ?? "",
      ) !== path.resolve(slot.workspacePath) ||
      resolveRepositoryIdentity(slot.workspacePath).repositoryId !==
        repositoryId ||
      git(slot.workspacePath, [
        "symbolic-ref",
        "--quiet",
        "--short",
        "HEAD",
      ]) !== slot.branchName ||
      git(slot.workspacePath, ["rev-parse", "HEAD"]) !== branchOid
    )
      throw new Error(
        `Role ${previousRole.name} worktree checkpoint diverged.`,
      );
    assertNoGitOperation(slot.workspacePath, previousRole.name);
    links = verifiedRecordedLinks(previousRole);
    assertCleanExceptLinks(slot.workspacePath, previousRole.name, links);
  } else {
    if (registeredCheckpoint) {
      if (
        !staleConduitRegistration &&
        (registeredCheckpoint.head !== branchOid ||
          registeredCheckpoint.branch !== slot.branchName)
      )
        throw new Error(
          `Role ${previousRole.name} missing worktree checkpoint diverged.`,
        );
      missingRegistration = true;
    } else if (!alreadyAbandoned && !unmaterialized)
      throw new Error(
        `Role ${previousRole.name} worktree disappeared before lineage preservation.`,
      );
  }
  return {
    previousRun,
    previousRole,
    nextRole,
    slot,
    branchOid,
    links,
    alreadyAbandoned,
    missingRegistration,
    missingBranch:
      (unmaterialized || staleConduitRegistration) && !observedBranchOid,
  };
}

export async function startNewRoleWorkspaces(input: {
  readonly projectRoot: string;
  readonly previousRuns: readonly Run[];
  readonly nextRun: Run;
  readonly repository: RoleWorkspaceRepository;
}): Promise<void> {
  const repositoryId = resolveRepositoryIdentity(
    input.projectRoot,
  ).repositoryId;
  const previousById = new Map(
    input.previousRuns.map((run) => [run.id, run] as const),
  );
  const gathered = [];
  for (const nextRole of input.nextRun.roles) {
    if (!nextRole.workspaceRoleKey)
      throw new Error("Start Anew requires complete canonical role metadata.");
    let slot = await input.repository.load(
      repositoryId,
      nextRole.workspaceRoleKey,
    );
    if (!slot) continue;
    const previousRun = previousById.get(slot.owningRunId);
    const slotRoleKey = slot.roleKey;
    const previousRole = previousRun?.roles.find(
      (role) => role.workspaceRoleKey === slotRoleKey,
    );
    if (!previousRun || !previousRole)
      throw new Error(
        `Retained role ${slot.roleKey} has no canonical run context.`,
      );
    slot = await reconcileFailedProvisioningSlot({
      projectRoot: input.projectRoot,
      previousRun,
      previousRole,
      slot,
      repository: input.repository,
    });
    gathered.push(
      await preflightContext({
        projectRoot: input.projectRoot,
        repositoryId,
        previousRun,
        previousRole,
        nextRun: input.nextRun,
        nextRole,
        slot,
        repository: input.repository,
      }),
    );
  }

  const claims = await input.repository.claimAll(
    gathered.map(({ previousRole, slot }) => ({
      repositoryId,
      roleKey: slot.roleKey,
      workspacePath: slot.workspacePath,
      owningRunId: slot.owningRunId,
      startingHead: slot.startingHead,
      packageHash: slot.packageHash,
      assignmentHash: slot.assignmentHash,
      branchName: slot.branchName,
      leaseOwner: previousRole.workspaceLeaseOwner!,
    })),
  );
  if (claims.status !== "claimed")
    throw new Error(
      `Retained role workspaces are leased by run ${claims.owningRunId}.`,
    );
  const claimedByRole = new Map(
    claims.slots.map((slot) => [slot.roleKey, slot]),
  );

  try {
    const verified = [];
    for (const context of gathered) {
      const claimed = claimedByRole.get(context.slot.roleKey);
      if (!claimed)
        throw new Error(`Role ${context.slot.roleKey} was not claimed.`);
      verified.push({
        ...(await preflightContext({
          projectRoot: input.projectRoot,
          repositoryId,
          previousRun: context.previousRun,
          previousRole: context.previousRole,
          nextRun: input.nextRun,
          nextRole: context.nextRole,
          slot: claimed,
          repository: input.repository,
        })),
        previous: {
          repositoryId,
          roleKey: claimed.roleKey,
          owningRunId: claimed.owningRunId,
          leaseOwner: claimed.leaseOwner!,
          fencingToken: claimed.fencingToken,
        } satisfies RoleWorkspaceLeaseIdentity,
      });
    }

    const advances: RoleWorkspaceAdvanceInput[] = [];
    for (const context of verified) {
      if (context.missingBranch) {
        const created = spawnSync(
          "git",
          [
            "-C",
            input.projectRoot,
            "update-ref",
            `refs/heads/${context.slot.branchName}`,
            context.branchOid,
            "",
          ],
          { encoding: "utf8" },
        );
        if (created.status !== 0)
          throw new Error(
            `Role ${context.previousRole.name} branch checkpoint could not be preserved.`,
          );
      }
      if (!context.alreadyAbandoned) {
        if (
          !(await input.repository.completeGeneration(context.previous, {
            branchOid: context.branchOid,
            outcome: "abandoned",
          }))
        )
          throw new Error(
            `Role ${context.previousRole.name} lineage could not be preserved.`,
          );
      }
      if (existsSync(context.slot.workspacePath)) {
        for (const link of context.links)
          rmSync(path.resolve(context.slot.workspacePath, link), {
            recursive: true,
            force: true,
          });
        assertCleanExceptLinks(
          context.slot.workspacePath,
          context.previousRole.name,
          [],
        );
      }
      if (
        existsSync(context.slot.workspacePath) ||
        context.missingRegistration
      ) {
        const removed = gitWorktreeRegistry.remove(
          input.projectRoot,
          context.slot.workspacePath,
        );
        if (!removed)
          throw new Error(
            `Role ${context.previousRole.name} worktree could not be removed.`,
          );
      }
      advances.push({
        previous: context.previous,
        next: {
          repositoryId,
          roleKey: context.nextRole.workspaceRoleKey!,
          workspacePath: context.nextRole.worktree!,
          owningRunId: input.nextRun.id,
          startingHead: input.nextRun.startingHead!,
          packageHash: input.nextRun.featurePackageHash!,
          assignmentHash: context.nextRole.workspaceAssignmentHash!,
          branchName: context.nextRole.workspaceBranchName!,
          leaseOwner: context.nextRole.workspaceLeaseOwner!,
        },
      });
    }

    const slots = await input.repository.advanceAll(advances);
    const rolesByKey = new Map(
      input.nextRun.roles.flatMap((role) =>
        role.workspaceRoleKey ? [[role.workspaceRoleKey, role] as const] : [],
      ),
    );
    for (const slot of slots) {
      const role = rolesByKey.get(slot.roleKey);
      if (!role)
        throw new Error(`Advanced role slot ${slot.roleKey} is unexpected.`);
      role.workspaceFencingToken = slot.fencingToken;
    }
  } catch (cause) {
    await Promise.all(
      claims.slots.flatMap((slot) =>
        slot.leaseOwner
          ? [
              input.repository.retain(
                {
                  repositoryId: slot.repositoryId,
                  roleKey: slot.roleKey,
                  owningRunId: slot.owningRunId,
                  leaseOwner: slot.leaseOwner,
                  fencingToken: slot.fencingToken,
                },
                slot.worktreeHead ?? slot.startingHead,
              ),
            ]
          : [],
      ),
    ).catch(() => undefined);
    throw cause;
  }
}
