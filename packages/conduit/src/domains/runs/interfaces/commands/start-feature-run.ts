import type { Command } from "../../../../system/bus/command-bus.js";

export interface StartFeatureRunCommand extends Command {
  readonly type: "startFeatureRun";
  readonly featureId: string;
  readonly roleNames: readonly string[];
  readonly mode: "continue" | "start-new";
  readonly confirmDiscardRetained?: boolean;
  readonly waitForCompletion?: boolean;
  readonly dryRun?: boolean;
  readonly fetchSkills?: boolean;
}

export interface StartFeatureRunResult {
  readonly runId: string;
  readonly results?: readonly import("../../types/run.js").RunResult[];
}
