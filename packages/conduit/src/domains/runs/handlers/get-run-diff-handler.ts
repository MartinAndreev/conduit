import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetRunDiffQuery,
  GetRunDiffReadModel,
} from "../interfaces/queries/get-run-diff.js";
import type { DiffReader } from "../interfaces/diff-reader.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";

export function createGetRunDiffHandler(
  diffReader: DiffReader,
  recoveryRepository: RunRecoveryRepository,
): QueryHandler<GetRunDiffQuery, GetRunDiffReadModel> {
  return async (query) => {
    try {
      const snapshot = await recoveryRepository.loadSnapshot(query.runId);
      const role = snapshot?.run.roles.find(
        (item) => item.name === query.roleId,
      );
      const worktree = role?.worktree ?? "";

      if (!worktree) {
        return {
          success: true,
          data: {
            diff: undefined,
            changedFiles: [],
            totalAdditions: 0,
            totalDeletions: 0,
          },
        };
      }

      const diffResult = diffReader.readDiff(worktree);
      return {
        success: true,
        data: {
          diff: diffResult.diff,
          changedFiles: diffResult.changedFiles,
          totalAdditions: diffResult.totalAdditions,
          totalDeletions: diffResult.totalDeletions,
        },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "DIFF_READ_ERROR",
          message: `Failed to read worktree diff: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
