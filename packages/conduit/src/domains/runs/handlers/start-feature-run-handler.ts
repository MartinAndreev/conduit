import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { Config } from "../../configuration/types/config.js";
import type {
  StartFeatureRunCommand,
  StartFeatureRunResult,
} from "../interfaces/commands/start-feature-run.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import type { RuntimeEventRepository } from "../interfaces/runtime-event-repository.js";
import type { RunProcessRegistry } from "../repositories/run-process-registry.js";
import type { Run, RunResult } from "../types/run.js";
import type { WorkspaceContinuity } from "../types/workspace-continuity.js";
import { startNewRoleWorkspaces } from "../services/start-new-role-workspaces-service.js";
import { retainClaimedRoleWorkspaces } from "../services/retain-role-workspaces-service.js";

export function createStartFeatureRunHandler(dependencies: {
  readonly projectRoot: string;
  readonly builtinRoot: string;
  readonly loadConfig: (projectRoot: string) => Promise<Config>;
  readonly planRun: (input: {
    projectRoot: string;
    config: Config;
    featureId: string;
    roleNames: string[];
    builtinRoot: string;
    fetchSkills?: boolean;
  }) => Promise<{ run: Run; runDir: string }>;
  readonly executeRun: (input: {
    projectRoot: string;
    run: Run;
    runDir: string;
    dryRun: boolean;
    eventRepository?: RunEventRepository;
    resultRepository?: ConduitResultRecordRepository;
    runtimeEventRepository?: RuntimeEventRepository;
    processRegistry?: RunProcessRegistry;
    onRoleWorkspaceReady?: () => Promise<void>;
    roleWorkspaceRepository?: RoleWorkspaceRepository;
  }) => Promise<RunResult[]>;
  readonly recoveryRepository: RunRecoveryRepository;
  readonly roleWorkspaceRepository: RoleWorkspaceRepository;
  readonly eventRepository?: RunEventRepository;
  readonly resultRepository?: ConduitResultRecordRepository;
  readonly runtimeEventRepository?: RuntimeEventRepository;
  readonly processRegistry?: RunProcessRegistry;
  readonly getContinuity: (
    featureId: string,
    roleNames: readonly string[],
  ) => Promise<WorkspaceContinuity>;
  readonly resumeRun: (
    runId: string,
  ) => Promise<
    | { success: true }
    | { success: false; error: { code: string; message: string } }
  >;
}): CommandHandler<StartFeatureRunCommand, StartFeatureRunResult> {
  return async (command) => {
    const roleNames = [...command.roleNames];
    if (!roleNames.length)
      return {
        success: false,
        error: {
          code: "NO_RUN_ROLES",
          message: "Configure at least one role before starting a run.",
        },
      };
    const continuity = await dependencies.getContinuity(
      command.featureId,
      roleNames,
    );
    if (command.mode === "continue") {
      if (command.dryRun)
        return {
          success: false,
          error: {
            code: "CONTINUE_DRY_RUN_UNSUPPORTED",
            message: "Continue cannot be combined with dry-run.",
          },
        };
      if (continuity.state !== "compatible-continue")
        return {
          success: false,
          error: {
            code: "WORKSPACE_NOT_CONTINUABLE",
            message:
              "reason" in continuity
                ? continuity.reason
                : "No compatible retained run is available.",
          },
        };
      const resumed = await dependencies.resumeRun(continuity.runId);
      return resumed.success
        ? { success: true, data: { runId: continuity.runId } }
        : resumed;
    }
    if (continuity.state === "lease-conflict")
      return {
        success: false,
        error: { code: "ROLE_WORKSPACE_LEASED", message: continuity.reason },
      };
    if (
      !command.dryRun &&
      continuity.state !== "no-retained" &&
      command.confirmDiscardRetained !== true
    )
      return {
        success: false,
        error: {
          code: "START_NEW_CONFIRMATION_REQUIRED",
          message: "Start Anew requires explicit retained-work confirmation.",
        },
      };

    const config = await dependencies.loadConfig(dependencies.projectRoot);
    const { run, runDir } = await dependencies.planRun({
      projectRoot: dependencies.projectRoot,
      config,
      featureId: command.featureId,
      roleNames,
      builtinRoot: dependencies.builtinRoot,
      fetchSkills: command.fetchSkills,
    });
    if (!command.dryRun && continuity.state !== "no-retained")
      run.status = "failed";
    const initial = await dependencies.recoveryRepository.saveSnapshot(run);
    let snapshotVersion = initial.version;
    if (!command.dryRun && continuity.state !== "no-retained") {
      const runIds =
        continuity.state === "incompatible-retained"
          ? continuity.runIds
          : [continuity.runId];
      const previous = await Promise.all(
        runIds.map((runId) =>
          dependencies.recoveryRepository.loadSnapshot(runId),
        ),
      );
      if (previous.some((snapshot) => !snapshot))
        return {
          success: false,
          error: {
            code: "RETAINED_RUN_NOT_FOUND",
            message: "Retained run state is unavailable.",
          },
        };
      try {
        await startNewRoleWorkspaces({
          projectRoot: dependencies.projectRoot,
          previousRuns: previous.map((snapshot) => snapshot!.run),
          nextRun: run,
          repository: dependencies.roleWorkspaceRepository,
        });
        run.status = "planned";
        snapshotVersion = (
          await dependencies.recoveryRepository.saveSnapshot(
            run,
            snapshotVersion,
          )
        ).version;
      } catch (cause) {
        const diagnostic =
          cause instanceof Error ? cause.message : String(cause);
        run.status = "failed";
        for (const role of run.roles) role.status = "failed";
        await retainClaimedRoleWorkspaces(
          run,
          dependencies.roleWorkspaceRepository,
        ).catch(() => undefined);
        await dependencies.recoveryRepository
          .saveSnapshot(run, snapshotVersion)
          .catch(() => undefined);
        await dependencies.recoveryRepository
          .markInterrupted(run.id, diagnostic)
          .catch(() => undefined);
        return {
          success: false,
          error: {
            code: "START_NEW_FAILED",
            message: diagnostic,
          },
        };
      }
    }
    let snapshotWrite = Promise.resolve();
    const persist = (): Promise<void> => {
      snapshotWrite = snapshotWrite.then(async () => {
        snapshotVersion = (
          await dependencies.recoveryRepository.saveSnapshot(
            run,
            snapshotVersion,
          )
        ).version;
      });
      return snapshotWrite;
    };
    const execute = async (): Promise<RunResult[]> => {
      try {
        const results = await dependencies.executeRun({
          projectRoot: dependencies.projectRoot,
          run,
          runDir,
          dryRun: Boolean(command.dryRun),
          eventRepository: dependencies.eventRepository,
          resultRepository: dependencies.resultRepository,
          runtimeEventRepository: dependencies.runtimeEventRepository,
          processRegistry: dependencies.processRegistry,
          onRoleWorkspaceReady: persist,
          roleWorkspaceRepository: dependencies.roleWorkspaceRepository,
        });
        await persist();
        if (run.status === "cancelled")
          await dependencies.recoveryRepository.markCancelled(run.id);
        return results;
      } catch (cause) {
        run.status = "failed";
        for (const role of run.roles)
          if (role.status !== "completed") role.status = "failed";
        await retainClaimedRoleWorkspaces(
          run,
          dependencies.roleWorkspaceRepository,
        ).catch(() => undefined);
        await persist().catch(() => undefined);
        await dependencies.recoveryRepository.markInterrupted(
          run.id,
          cause instanceof Error ? cause.message : String(cause),
        );
        throw cause;
      }
    };
    if (command.waitForCompletion) {
      const results = await execute();
      return { success: true, data: { runId: run.id, results } };
    }
    void execute().catch(() => undefined);
    return { success: true, data: { runId: run.id } };
  };
}
