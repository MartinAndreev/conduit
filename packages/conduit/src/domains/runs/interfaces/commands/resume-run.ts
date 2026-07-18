import type { Command } from "../../../../system/bus/command-bus.js";

export interface ResumeRunCommand extends Command {
  readonly type: "resumeRun";
  readonly runId: string;
}

export interface ResumeRunResult {
  readonly runId: string;
  readonly resumed: boolean;
}
