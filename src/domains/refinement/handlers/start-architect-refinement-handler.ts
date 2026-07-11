import path from "node:path";
import { mkdir, writeFile } from "node:fs/promises";
import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { Config } from "../../configuration/types/config.js";
import type { Feature } from "../../features/types/feature.js";
import type {
  StartArchitectRefinementCommand,
  StartArchitectRefinementResult,
} from "../interfaces/commands/start-architect-refinement.js";

export interface StartArchitectRefinementDependencies {
  readonly projectRoot: string;
  readonly loadConfig: (projectRoot: string) => Promise<Config>;
  readonly findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  readonly refinementPrompt: (feature: Feature, story: string) => string;
  readonly runArchitect: (params: {
    projectRoot: string;
    prompt: string;
    logFile: string;
  }) => Promise<{ logFile: string }>;
}

export function createStartArchitectRefinementHandler(
  deps: StartArchitectRefinementDependencies,
): CommandHandler<
  StartArchitectRefinementCommand,
  StartArchitectRefinementResult
> {
  return async (command) => {
    try {
      const config = await deps.loadConfig(deps.projectRoot);
      const feature = await deps.findFeature({
        projectRoot: deps.projectRoot,
        config,
        featureId: command.featureId,
      });
      const runId = `refine-${feature.id}-${Date.now()}`;
      const logFile = path.join(
        deps.projectRoot,
        config.stateDir ?? ".conduit",
        "runs",
        runId,
        "architect.log",
      );
      await mkdir(path.dirname(logFile), { recursive: true });
      await writeFile(logFile, "analysis\n");
      const result = await deps.runArchitect({
        projectRoot: deps.projectRoot,
        prompt: deps.refinementPrompt(feature, command.story),
        logFile,
      });
      return { success: true, data: { runId, logFile: result.logFile } };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "START_ARCHITECT_REFINEMENT_ERROR",
          message: error instanceof Error ? error.message : String(error),
          cause: error,
        },
      };
    }
  };
}
