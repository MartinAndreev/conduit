import type { Query } from "../../../../system/bus/query-bus.js";
import type { RunnerEvent } from "../../types/runner-events.js";

export interface GetRunEventsQuery extends Query {
  readonly type: "getRunEvents";
  readonly runId: string;
  readonly roleId?: string;
}

export interface GetRunEventsReadModel {
  readonly events: readonly RunnerEvent[];
  readonly roleIds: readonly string[];
}
