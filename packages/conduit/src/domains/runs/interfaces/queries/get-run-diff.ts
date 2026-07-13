import type { Query } from "../../../../system/bus/query-bus.js";
import type { ChangedFile, RunDiffResult } from "../../types/review.js";

export interface GetRunDiffQuery extends Query {
  readonly type: "getRunDiff";
  readonly projectRoot: string;
  readonly runId: string;
  readonly roleId: string;
}

export type { ChangedFile, RunDiffResult as GetRunDiffReadModel };
