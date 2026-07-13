import type { Command } from "@system/bus/command-bus.js";

export interface StartResearchRefinementCommand extends Command {
  readonly type: "startResearchRefinement";
  readonly featureId: string;
  readonly story: string;
}

export interface StartResearchRefinementResult {
  readonly runId: string;
  readonly reportFile: string;
}
