import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  DiscardDraftCommand,
  DiscardDraftResult,
} from "../interfaces/commands/discard-draft.js";
import type { DraftRepository } from "../interfaces/draft-repository.js";

export function createDiscardDraftHandler(
  draftRepository: DraftRepository,
): CommandHandler<DiscardDraftCommand, DiscardDraftResult> {
  return async (command) => {
    try {
      const discarded = await draftRepository.discard(command.featureId);
      return {
        success: true,
        data: { discarded },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "DRAFT_DISCARD_ERROR",
          message: `Failed to discard draft: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
