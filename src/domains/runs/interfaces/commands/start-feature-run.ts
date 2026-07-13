import type { Command } from "../../../../system/bus/command-bus.js";

export interface StartFeatureRunCommand extends Command {
  readonly type: "startFeatureRun";
  readonly featureId: string;
  readonly roleNames: readonly string[];
}

export interface StartFeatureRunResult {
  readonly runId: string;
}
