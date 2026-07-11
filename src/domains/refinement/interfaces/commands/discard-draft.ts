import type { Command } from "../../../../system/bus/command-bus.js";

export interface DiscardDraftCommand extends Command {
  readonly type: "discardDraft";
  readonly featureId: string;
}

export interface DiscardDraftResult {
  readonly discarded: boolean;
}
