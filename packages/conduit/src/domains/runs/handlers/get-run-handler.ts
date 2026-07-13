import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetRunQuery,
  GetRunReadModel,
} from "../interfaces/queries/get-run.js";
import type { Run } from "../types/run.js";
import { readFile } from "node:fs/promises";
import path from "node:path";

export function createGetRunHandler(
  loadConfig: (projectRoot: string) => Promise<{ stateDir: string }>,
): QueryHandler<GetRunQuery, GetRunReadModel> {
  return async (query) => {
    try {
      const config = await loadConfig(query.projectRoot);
      const runFile = path.join(
        query.projectRoot,
        config.stateDir,
        "runs",
        query.runId,
        "run.json",
      );
      const raw = await readFile(runFile, "utf8");
      const run: Run = JSON.parse(raw);
      return { success: true, data: { run } };
    } catch {
      return { success: true, data: { run: undefined } };
    }
  };
}
