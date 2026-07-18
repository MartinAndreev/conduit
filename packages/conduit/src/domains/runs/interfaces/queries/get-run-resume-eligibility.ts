import type { Query } from "../../../../system/bus/query-bus.js";
import type { ResumeEligibility } from "../../types/resume-eligibility.js";

export interface GetRunResumeEligibilityQuery extends Query {
  readonly type: "getRunResumeEligibility";
  readonly projectRoot: string;
  readonly runId: string;
}

export type GetRunResumeEligibilityReadModel = ResumeEligibility;
