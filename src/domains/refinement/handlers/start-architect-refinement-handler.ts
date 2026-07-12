import path from "node:path";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import type { Config } from "@domains/configuration/types/config.js";
import type { Feature } from "@domains/features/types/feature.js";
import type {
  StartArchitectRefinementCommand,
  StartArchitectRefinementResult,
} from "@domains/refinement/interfaces/commands/start-architect-refinement.js";
import type { RefinementRevisionRepository } from "@domains/refinement/interfaces/revision-repository.js";
import type { CommandHandler } from "@system/bus/command-bus.js";

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
  readonly revisionRepository: RefinementRevisionRepository;
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
      const latest = await deps.revisionRepository.getLatest(feature);
      const revision =
        latest && command.revisionId === latest.id
          ? await deps.revisionRepository.updateStatus(latest, "running")
          : await deps.revisionRepository.create(feature);
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
        prompt: deps.refinementPrompt(
          feature,
          revision.feedback
            ? `${command.story}\n\nPrior packet review feedback (preserve already-approved decisions unless this feedback changes them):\n${revision.feedback}`
            : command.story,
        ),
        logFile,
      });
      const [transcript, questions] = await Promise.all([
        readFile(result.logFile, "utf8").catch(() => ""),
        readFile(path.join(feature.directory, "questions.md"), "utf8").catch(
          () => "",
        ),
      ]);
      await deps.revisionRepository.recordRun(revision, transcript);
      if (questions.trim()) {
        await deps.revisionRepository.saveQuestions(revision, questions);
        await deps.revisionRepository.updateStatus(
          revision,
          "awaiting_clarification",
        );
        return {
          success: true,
          data: {
            runId,
            logFile: result.logFile,
            revisionId: revision.id,
            status: "awaiting_clarification",
          },
        };
      }
      await deps.revisionRepository.updateStatus(revision, "ready_for_review");
      return {
        success: true,
        data: {
          runId,
          logFile: result.logFile,
          revisionId: revision.id,
          status: "ready_for_review",
        },
      };
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
