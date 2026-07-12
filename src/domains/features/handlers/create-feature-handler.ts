import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { Config } from "../../configuration/types/config.js";
import type { Feature } from "../types/feature.js";
import type {
  CreateFeatureCommand,
  CreateFeatureResult,
} from "../interfaces/commands/create-feature.js";

export function createCreateFeatureHandler(deps: {
  readonly projectRoot: string;
  readonly loadConfig: (root: string) => Promise<Config>;
  readonly createFeature: (params: {
    projectRoot: string;
    config: Config;
    title: string;
  }) => Promise<Feature>;
}): CommandHandler<CreateFeatureCommand, CreateFeatureResult> {
  return async (command) => {
    if (!command.title.trim())
      return {
        success: false,
        error: {
          code: "FEATURE_TITLE_REQUIRED",
          message: "A feature title is required.",
        },
      };
    try {
      const config = await deps.loadConfig(deps.projectRoot);
      const feature = await deps.createFeature({
        projectRoot: deps.projectRoot,
        config,
        title: command.title,
      });
      return { success: true, data: feature };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "CREATE_FEATURE_ERROR",
          message: error instanceof Error ? error.message : String(error),
          cause: error,
        },
      };
    }
  };
}
