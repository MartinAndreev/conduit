import type { Command } from "../../../../system/bus/command-bus.js";

export interface CancelRunCommand extends Command {
  readonly type: "cancelRun";
  readonly runId: string;
}

export interface CancelRunResult {
  readonly runId: string;
  readonly cancelled: boolean;
}
