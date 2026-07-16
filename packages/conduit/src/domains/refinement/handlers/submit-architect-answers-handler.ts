import { appendFile, rm } from "node:fs/promises";
import path from "node:path";
import type { CommandHandler } from "@system/bus/command-bus.js";
import type { Config } from "@domains/configuration/types/config.js";
import type { Feature } from "@domains/features/types/feature.js";
import type { RefinementRevisionRepository } from "@domains/refinement/interfaces/revision-repository.js";
import type { ClarificationQuestionRepository } from "@domains/refinement/interfaces/clarification-question-repository.js";
import type {
  SubmitArchitectAnswersCommand,
  SubmitArchitectAnswersResult,
} from "@domains/refinement/interfaces/commands/submit-architect-answers.js";

export function createSubmitArchitectAnswersHandler(deps: {
  projectRoot: string;
  loadConfig: (projectRoot: string) => Promise<Config>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  repository: RefinementRevisionRepository;
  clarificationQuestionRepository?: ClarificationQuestionRepository;
}): CommandHandler<
  SubmitArchitectAnswersCommand,
  SubmitArchitectAnswersResult
> {
  return async (command) => {
    if (!command.answers.trim())
      return {
        success: false,
        error: {
          code: "EMPTY_ANSWERS",
          message: "Provide an answer before resuming the architect.",
        },
      };
    try {
      const config = await deps.loadConfig(deps.projectRoot);
      const feature = await deps.findFeature({
        projectRoot: deps.projectRoot,
        config,
        featureId: command.featureId,
      });
      const revision = await deps.repository.getLatest(feature);
      if (!revision || revision.id !== command.revisionId)
        throw new Error("The clarification revision is no longer current.");
      await deps.repository.saveAnswers(revision, command.answers);
      await deps.clarificationQuestionRepository?.answerUnresolved(
        feature.id,
        revision.id,
        command.answers,
      );
      await appendFile(
        path.join(feature.directory, "clarifications.md"),
        `\n## ${revision.id}\n\n${command.answers.trim()}\n`,
      );
      await rm(path.join(feature.directory, "questions.md"), { force: true });
      await deps.repository.updateStatus(revision, "running");
      return { success: true, data: { accepted: true } };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "SUBMIT_ARCHITECT_ANSWERS_ERROR",
          message: error instanceof Error ? error.message : String(error),
          cause: error,
        },
      };
    }
  };
}
