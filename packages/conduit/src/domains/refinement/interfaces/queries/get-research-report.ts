import type { Query } from "@system/bus/query-bus.js";

export interface GetResearchReportQuery extends Query {
  readonly type: "getResearchReport";
  readonly featureId: string;
}

export interface GetResearchReportResult {
  readonly report: string | null;
  readonly reportFile: string;
}
