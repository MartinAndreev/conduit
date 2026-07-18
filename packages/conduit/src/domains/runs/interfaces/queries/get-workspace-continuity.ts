import type { Query } from "../../../../system/bus/query-bus.js";
import type { WorkspaceContinuity } from "../../types/workspace-continuity.js";

export interface GetWorkspaceContinuityQuery extends Query {
  readonly type: "getWorkspaceContinuity";
  readonly featureId: string;
  readonly roleNames: readonly string[];
}

export type GetWorkspaceContinuityReadModel = WorkspaceContinuity;
