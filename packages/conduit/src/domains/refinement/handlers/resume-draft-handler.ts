import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  ResumeDraftCommand,
  ResumeDraftResult,
} from "../interfaces/commands/resume-draft.js";
import type { DraftRepository } from "../interfaces/draft-repository.js";

export function createResumeDraftHandler(
  draftRepository: DraftRepository,
): CommandHandler<ResumeDraftCommand, ResumeDraftResult> {
  return async (command) => {
    try {
      const draft = await draftRepository.load(command.featureId);
      if (!draft) {
        return {
          success: false,
          error: {
            code: "DRAFT_NOT_FOUND",
            message: `No draft found for feature: ${command.featureId}`,
          },
        };
      }

      return {
        success: true,
        data: { resumed: true, draftPath: `${command.featureId}.json` },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "DRAFT_RESUME_ERROR",
          message: `Failed to resume draft: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
