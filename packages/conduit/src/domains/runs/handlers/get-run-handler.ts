import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetRunQuery,
  GetRunReadModel,
} from "../interfaces/queries/get-run.js";
import type { Run } from "../types/run.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";

export function createGetRunHandler(
  repository: RunRecoveryRepository,
): QueryHandler<GetRunQuery, GetRunReadModel> {
  return async (query) => {
    try {
      const run: Run | undefined = (await repository.loadSnapshot(query.runId))
        ?.run;
      return { success: true, data: { run } };
    } catch {
      return { success: true, data: { run: undefined } };
    }
  };
}
