import { readFile } from "node:fs/promises";
import path from "node:path";
import type { Config } from "@domains/configuration/types/config.js";
import type { Feature } from "@domains/features/types/feature.js";
import type {
  GetResearchReportQuery,
  GetResearchReportResult,
} from "@domains/refinement/interfaces/queries/get-research-report.js";
import type { QueryHandler } from "@system/bus/query-bus.js";

export interface GetResearchReportDependencies {
  readonly projectRoot: string;
  readonly loadConfig: (projectRoot: string) => Promise<Config>;
  readonly findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
}

export function createGetResearchReportHandler(
  deps: GetResearchReportDependencies,
): QueryHandler<GetResearchReportQuery, GetResearchReportResult> {
  return async (query) => {
    try {
      const config = await deps.loadConfig(deps.projectRoot);
      const feature = await deps.findFeature({
        projectRoot: deps.projectRoot,
        config,
        featureId: query.featureId,
      });
      const reportFile = path.join(feature.directory, "research.md");
      const report = await readFile(reportFile, "utf8").catch(() => null);
      return { success: true, data: { report, reportFile } };
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
