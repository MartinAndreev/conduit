import type { Command } from "../../../../system/bus/command-bus.js";

export interface SaveDraftCommand extends Command {
  readonly type: "saveDraft";
  readonly featureId: string;
  readonly story: string;
  readonly testCases: string;
  readonly expectedVersion?: number;
}

export interface SaveDraftResult {
  readonly saved: boolean;
  readonly draftPath: string;
}
