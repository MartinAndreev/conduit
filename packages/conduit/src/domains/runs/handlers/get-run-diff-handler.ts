import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetRunDiffQuery,
  GetRunDiffReadModel,
} from "../interfaces/queries/get-run-diff.js";
import type { DiffReader } from "../repositories/worktree-diff-reader.js";
import type { Run } from "../types/run.js";
import { readFile } from "node:fs/promises";
import path from "node:path";

export function createGetRunDiffHandler(
  diffReader: DiffReader,
  loadConfig: (projectRoot: string) => Promise<{ stateDir: string }>,
): QueryHandler<GetRunDiffQuery, GetRunDiffReadModel> {
  return async (query) => {
    try {
      // Resolve worktree from persisted run data, not from the query
      const config = await loadConfig(query.projectRoot);
      const runFile = path.join(
        query.projectRoot,
        config.stateDir,
        "runs",
        query.runId,
        "run.json",
      );
      let worktree = "";
      try {
        const raw = await readFile(runFile, "utf8");
        const run: Run = JSON.parse(raw);
        const role = run.roles.find((r) => r.name === query.roleId);
        worktree = role?.worktree ?? "";
      } catch {
        // Run file doesn't exist
      }

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
