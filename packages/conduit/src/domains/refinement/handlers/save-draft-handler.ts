import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  SaveDraftCommand,
  SaveDraftResult,
} from "../interfaces/commands/save-draft.js";
import type { DraftRepository } from "../interfaces/draft-repository.js";

export function createSaveDraftHandler(
  draftRepository: DraftRepository,
): CommandHandler<SaveDraftCommand, SaveDraftResult> {
  return async (command) => {
    try {
      const draft = {
        featureId: command.featureId,
        story: command.story,
        testCases: command.testCases,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      };

      const draftPath = await draftRepository.save(draft);
      return {
        success: true,
        data: { saved: true, draftPath },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "DRAFT_SAVE_ERROR",
          message: `Failed to save draft: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
