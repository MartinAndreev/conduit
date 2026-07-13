import type { Query } from "../../../../system/bus/query-bus.js";
import type { Run } from "../../types/run.js";

export interface GetRunQuery extends Query {
  readonly type: "getRun";
  readonly projectRoot: string;
  readonly runId: string;
}

export interface GetRunReadModel {
  readonly run: Run | undefined;
}
