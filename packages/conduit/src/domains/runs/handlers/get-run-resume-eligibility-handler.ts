import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type {
  GetRunResumeEligibilityQuery,
  GetRunResumeEligibilityReadModel,
} from "../interfaces/queries/get-run-resume-eligibility.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import { evaluateRunResumeEligibility } from "../services/run-resume-eligibility-service.js";

export function createGetRunResumeEligibilityHandler(
  recoveryRepository: RunRecoveryRepository,
  resultRepository?: ConduitResultRecordRepository,
  roleWorkspaceRepository?: import("../interfaces/role-workspace-repository.js").RoleWorkspaceRepository,
): QueryHandler<
  GetRunResumeEligibilityQuery,
  GetRunResumeEligibilityReadModel
> {
  return async (query) => {
    const snapshot = await recoveryRepository.loadSnapshot(query.runId);
    if (!snapshot)
      return {
        success: true,
        data: {
          state: "not-resumable" as const,
          reason: "Run was not found.",
          preservedRoles: [],
          retryRoles: [],
          reconstructRoles: [],
        },
      };
    return {
      success: true,
      data: await evaluateRunResumeEligibility({
        projectRoot: query.projectRoot,
        run: snapshot.run,
        resultRepository,
        roleWorkspaceRepository,
      }),
    };
  };
}
