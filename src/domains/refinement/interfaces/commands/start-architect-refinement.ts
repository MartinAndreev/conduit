import type { Command } from "../../../../system/bus/command-bus.js";

export interface StartArchitectRefinementCommand extends Command {
  readonly type: "startArchitectRefinement";
  readonly featureId: string;
  readonly story: string;
}

export interface StartArchitectRefinementResult {
  readonly runId: string;
  readonly logFile: string;
}
