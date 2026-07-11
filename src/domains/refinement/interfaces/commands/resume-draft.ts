import type { Command } from "../../../../system/bus/command-bus.js";

export interface ResumeDraftCommand extends Command {
  readonly type: "resumeDraft";
  readonly featureId: string;
}

export interface ResumeDraftResult {
  readonly resumed: boolean;
  readonly draftPath: string;
}
