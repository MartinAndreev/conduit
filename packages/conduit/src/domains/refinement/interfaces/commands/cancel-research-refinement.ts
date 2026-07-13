import type { Command } from "@system/bus/command-bus.js";

export interface CancelResearchRefinementCommand extends Command {
  readonly type: "cancelResearchRefinement";
  readonly featureId: string;
}

export interface CancelResearchRefinementResult {
  readonly cancelled: boolean;
}
