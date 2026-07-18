import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import type { WorkspaceContinuity } from "../types/workspace-continuity.js";
import { evaluateRunResumeEligibility } from "./run-resume-eligibility-service.js";
import {
  normalizeRoleWorkspaceKey,
  resolveRepositoryIdentity,
} from "./role-workspace-identity-service.js";

export async function evaluateWorkspaceContinuity(input: {
  readonly projectRoot: string;
  readonly featureId: string;
  readonly roleNames: readonly string[];
  readonly recoveryRepository: RunRecoveryRepository;
  readonly roleWorkspaceRepository: RoleWorkspaceRepository;
  readonly resultRepository?: ConduitResultRecordRepository;
}): Promise<WorkspaceContinuity> {
  const roles = [...new Set(input.roleNames)].sort((a, b) =>
    a.localeCompare(b),
  );
  const repositoryId = resolveRepositoryIdentity(
    input.projectRoot,
  ).repositoryId;
  const slots = (
    await Promise.all(
      roles.map((role) =>
        input.roleWorkspaceRepository.load(
          repositoryId,
          normalizeRoleWorkspaceKey(role),
        ),
      ),
    )
  ).filter((slot) => slot !== undefined);
  if (!slots.length) return { state: "no-retained", roles };
  for (const leased of slots.filter((slot) => slot.leaseOwner)) {
    const owner = await input.recoveryRepository.loadSnapshot(
      leased.owningRunId,
    );
    const persistedRole = owner?.run.roles.find(
      (role) => role.workspaceRoleKey === leased.roleKey,
    );
    if (
      !owner ||
      !persistedRole ||
      persistedRole.workspaceLeaseOwner !== leased.leaseOwner ||
      owner.run.status === "planned" ||
      owner.run.status === "running" ||
      owner.run.status === "awaiting-input"
    )
      return {
        state: "lease-conflict",
        runId: leased.owningRunId,
        roles: slots.map((slot) => slot.roleKey),
        reason: `Role ${leased.roleKey} is leased by run ${leased.owningRunId}.`,
      };
  }
  const runIds = [...new Set(slots.map((slot) => slot.owningRunId))];
  if (runIds.length !== 1 || slots.length !== roles.length)
    return {
      state: "incompatible-retained",
      ...(runIds.length === 1 ? { runId: runIds[0] } : {}),
      runIds,
      roles: slots.map((slot) => slot.roleKey),
      reason: "Selected roles do not belong to one complete retained run.",
    };
  const runId = runIds[0]!;
  for (const slot of slots) {
    const generation = (
      await input.roleWorkspaceRepository.listGenerations(
        slot.repositoryId,
        slot.roleKey,
      )
    ).find((candidate) => candidate.generation === slot.generation);
    if (generation?.outcome === "abandoned")
      return {
        state: "incompatible-retained",
        runId,
        runIds,
        roles,
        reason: "A previous Start Anew operation must be completed.",
      };
  }
  const snapshot = await input.recoveryRepository.loadSnapshot(runId);
  if (!snapshot)
    return {
      state: "incompatible-retained",
      runId,
      runIds,
      roles,
      reason: "The retained workspace run snapshot is unavailable.",
    };
  if (snapshot.run.featureId !== input.featureId)
    return {
      state: "incompatible-retained",
      runId,
      runIds,
      roles,
      reason: `Retained work belongs to feature ${snapshot.run.featureId}.`,
    };
  const runRoles = snapshot.run.roles.map((role) => role.name).sort();
  if (JSON.stringify(runRoles) !== JSON.stringify(roles))
    return {
      state: "incompatible-retained",
      runId,
      runIds,
      roles,
      reason: "Selected roles differ from the retained run role graph.",
    };
  const eligibility = await evaluateRunResumeEligibility({
    projectRoot: input.projectRoot,
    run: snapshot.run,
    resultRepository: input.resultRepository,
    roleWorkspaceRepository: input.roleWorkspaceRepository,
  });
  if (eligibility.state !== "resumable")
    return {
      state: "incompatible-retained",
      runId,
      runIds,
      roles,
      reason: eligibility.reason ?? "Retained work is not safely continuable.",
    };
  return {
    state: "compatible-continue",
    runId,
    roles,
    preservedRoles: eligibility.preservedRoles,
    retryRoles: eligibility.retryRoles,
  };
}
