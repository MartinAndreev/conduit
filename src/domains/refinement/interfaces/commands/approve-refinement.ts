import type { Command } from "../../../../system/bus/command-bus.js";

export interface ApproveRefinementCommand extends Command {
  readonly type: "approveRefinement";
  readonly featureId: string;
  readonly story: string;
  readonly testCases: string;
}

export interface ApproveRefinementResult {
  readonly approved: boolean;
  readonly storyFile: string;
  readonly testCasesFile: string;
}
