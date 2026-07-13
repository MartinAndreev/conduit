import type { Command } from "../../../../system/bus/command-bus.js";
import type { ArchitectPreferences } from "../../types/architect-preferences.js";

export interface StartArchitectRefinementCommand extends Command {
  readonly type: "startArchitectRefinement";
  readonly featureId: string;
  readonly story: string;
  readonly preferences?: ArchitectPreferences;
  readonly revisionId?: string;
}

export interface StartArchitectRefinementResult {
  readonly runId: string;
  readonly logFile: string;
  readonly revisionId: string;
  readonly status: "awaiting_clarification" | "ready_for_review";
}
