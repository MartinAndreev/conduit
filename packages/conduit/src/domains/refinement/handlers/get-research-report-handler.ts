import type {
  GetResearchReportQuery,
  GetResearchReportResult,
} from "@domains/refinement/interfaces/queries/get-research-report.js";
import type { QueryHandler } from "@system/bus/query-bus.js";
import type { ResearchReportRepository } from "@domains/refinement/interfaces/research-report-repository.js";

export interface GetResearchReportDependencies {
  readonly repository: ResearchReportRepository;
}

export function createGetResearchReportHandler(
  deps: GetResearchReportDependencies,
): QueryHandler<GetResearchReportQuery, GetResearchReportResult> {
  return async (query) => {
    try {
      const result = await deps.repository.load(query.featureId);
      return {
        success: true,
        data: {
          report: result?.report ?? null,
          reportFile: `conduit://research/${encodeURIComponent(query.featureId)}`,
        },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "GET_RESEARCH_REPORT_ERROR",
          message: error instanceof Error ? error.message : String(error),
          cause: error,
        },
      };
    }
  };
}
