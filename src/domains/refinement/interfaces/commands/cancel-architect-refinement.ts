import type { Command } from "../../../../system/bus/command-bus.js";
export interface CancelArchitectRefinementCommand extends Command {
  readonly type: "cancelArchitectRefinement";
  readonly featureId: string;
}
export interface CancelArchitectRefinementResult {
  readonly cancelled: boolean;
}
