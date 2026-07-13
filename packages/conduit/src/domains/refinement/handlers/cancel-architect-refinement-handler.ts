import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  CancelArchitectRefinementCommand,
  CancelArchitectRefinementResult,
} from "../interfaces/commands/cancel-architect-refinement.js";
export function createCancelArchitectRefinementHandler(
  cancel: (featureId: string) => boolean,
): CommandHandler<
  CancelArchitectRefinementCommand,
  CancelArchitectRefinementResult
> {
  return async (command) => ({
    success: true,
    data: { cancelled: cancel(command.featureId) },
  });
}
