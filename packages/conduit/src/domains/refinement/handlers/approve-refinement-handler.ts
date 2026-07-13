import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  ApproveRefinementCommand,
  ApproveRefinementResult,
} from "../interfaces/commands/approve-refinement.js";
import type { Config } from "../../configuration/types/config.js";
import type { Feature } from "../../features/types/feature.js";

export interface ApproveRefinementDependencies {
  loadConfig: (projectRoot: string) => Promise<Config>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  writeStory: (feature: Feature, story: string) => Promise<string>;
  writeTestCases: (feature: Feature, testCases: string) => Promise<string>;
  projectRoot: string;
}

export function createApproveRefinementHandler(
  deps: ApproveRefinementDependencies,
): CommandHandler<ApproveRefinementCommand, ApproveRefinementResult> {
  return async (command) => {
    try {
      const config = await deps.loadConfig(deps.projectRoot);
      const feature = await deps.findFeature({
        projectRoot: deps.projectRoot,
        config,
        featureId: command.featureId,
      });

      const storyFile = await deps.writeStory(feature, command.story);
      const testCasesFile = await deps.writeTestCases(
        feature,
        command.testCases,
      );

      return {
        success: true,
        data: { approved: true, storyFile, testCasesFile },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "APPROVE_REFINEMENT_ERROR",
          message: `Failed to approve refinement: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
