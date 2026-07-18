import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type {
  GetWorkspaceContinuityQuery,
  GetWorkspaceContinuityReadModel,
} from "../interfaces/queries/get-workspace-continuity.js";
import type { RoleWorkspaceRepository } from "../interfaces/role-workspace-repository.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import { evaluateWorkspaceContinuity } from "../services/workspace-continuity-service.js";

export function createGetWorkspaceContinuityHandler(
  projectRoot: string,
  recoveryRepository: RunRecoveryRepository,
  roleWorkspaceRepository: RoleWorkspaceRepository,
  resultRepository?: ConduitResultRecordRepository,
): QueryHandler<GetWorkspaceContinuityQuery, GetWorkspaceContinuityReadModel> {
  return async (query) => ({
    success: true,
    data: await evaluateWorkspaceContinuity({
      projectRoot,
      featureId: query.featureId,
      roleNames: query.roleNames,
      recoveryRepository,
      roleWorkspaceRepository,
      resultRepository,
    }),
  });
}
