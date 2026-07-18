import path from "node:path";
import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  ResumeRunCommand,
  ResumeRunResult,
} from "../interfaces/commands/resume-run.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RuntimeEventRepository } from "../interfaces/runtime-event-repository.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type { RunProcessRegistry } from "../repositories/run-process-registry.js";
import type { Run, RunResult } from "../types/run.js";
import type { ResumeEligibility } from "../types/resume-eligibility.js";
import { evaluateRunResumeEligibility } from "../services/run-resume-eligibility-service.js";

export function createResumeRunHandler(
  recoveryRepository: RunRecoveryRepository,
  dependencies: {
    readonly projectRoot: string;
    readonly executeRun: (input: {
      projectRoot: string;
      run: Run;
      runDir: string;
      dryRun: boolean;
      resume: boolean;
      eventRepository?: RunEventRepository;
      runtimeEventRepository?: RuntimeEventRepository;
      resultRepository?: ConduitResultRecordRepository;
      processRegistry?: RunProcessRegistry;
      onRoleWorkspaceReady?: () => Promise<void>;
      roleWorkspaceRepository?: import("../interfaces/role-workspace-repository.js").RoleWorkspaceRepository;
    }) => Promise<RunResult[]>;
    readonly eventRepository?: RunEventRepository;
    readonly runtimeEventRepository?: RuntimeEventRepository;
    readonly resultRepository?: ConduitResultRecordRepository;
    readonly processRegistry?: RunProcessRegistry;
    readonly roleWorkspaceRepository?: import("../interfaces/role-workspace-repository.js").RoleWorkspaceRepository;
    readonly evaluateEligibility?: (input: {
      readonly projectRoot: string;
      readonly run: Run;
      readonly resultRepository?: ConduitResultRecordRepository;
      readonly roleWorkspaceRepository?: import("../interfaces/role-workspace-repository.js").RoleWorkspaceRepository;
    }) => Promise<ResumeEligibility>;
  },
): CommandHandler<ResumeRunCommand, ResumeRunResult> {
  return async (command) => {
    const snapshot = await recoveryRepository.loadSnapshot(command.runId);
    if (!snapshot)
      return {
        success: false,
        error: {
          code: "RUN_NOT_FOUND",
          message: `Run ${command.runId} was not found.`,
        },
      };
    const eligibility = await (
      dependencies.evaluateEligibility ?? evaluateRunResumeEligibility
    )({
      projectRoot: dependencies.projectRoot,
      run: snapshot.run,
      resultRepository: dependencies.resultRepository,
      roleWorkspaceRepository: dependencies.roleWorkspaceRepository,
    });
    if (eligibility.state !== "resumable")
      return {
        success: false,
        error: {
          code: "RUN_NOT_RESUMABLE",
          message:
            eligibility.reason ??
            `Run ${command.runId} cannot be resumed safely.`,
        },
      };

    const claimed = await recoveryRepository.claimFailedRun(
      command.runId,
      snapshot.version,
    );
    if (!claimed)
      return {
        success: false,
        error: {
          code: "RUN_RESUME_CONFLICT",
          message: `Run ${command.runId} was already resumed or updated.`,
        },
      };
    const run = claimed.run;
    const retryRoles = new Set(eligibility.retryRoles);
    for (const role of run.roles)
      if (retryRoles.has(role.name)) role.status = "failed";
    let version = claimed.version;
    let snapshotWrite = Promise.resolve();
    const persist = (): Promise<void> => {
      snapshotWrite = snapshotWrite
        .catch(() => undefined)
        .then(async () => {
          const saved = await recoveryRepository.saveSnapshot(run, version);
          version = saved.version;
        });
      return snapshotWrite;
    };
    try {
      await dependencies.executeRun({
        projectRoot: dependencies.projectRoot,
        run,
        runDir: path.join(
          run.stateDirectory ?? path.join(dependencies.projectRoot, ".conduit"),
          "runs",
          run.id,
        ),
        dryRun: false,
        resume: true,
        eventRepository: dependencies.eventRepository,
        runtimeEventRepository: dependencies.runtimeEventRepository,
        resultRepository: dependencies.resultRepository,
        processRegistry: dependencies.processRegistry,
        onRoleWorkspaceReady: persist,
        roleWorkspaceRepository: dependencies.roleWorkspaceRepository,
      });
      await persist();
      return {
        success: true,
        data: { runId: run.id, resumed: true },
      };
    } catch (cause) {
      run.status = "failed";
      await persist().catch(() => undefined);
      await recoveryRepository.markInterrupted(
        run.id,
        cause instanceof Error ? cause.message : String(cause),
      );
      return {
        success: false,
        error: {
          code: "RUN_RESUME_FAILED",
          message: cause instanceof Error ? cause.message : String(cause),
        },
      };
    }
  };
}
