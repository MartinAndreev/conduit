import type { CommandHandler } from "@system/bus/command-bus.js";
import type {
  CancelResearchRefinementCommand,
  CancelResearchRefinementResult,
} from "@domains/refinement/interfaces/commands/cancel-research-refinement.js";

export function createCancelResearchRefinementHandler(
  cancel: (featureId: string) => boolean,
): CommandHandler<
  CancelResearchRefinementCommand,
  CancelResearchRefinementResult
> {
  return async (command) => ({
    success: true,
    data: { cancelled: cancel(command.featureId) },
  });
}
