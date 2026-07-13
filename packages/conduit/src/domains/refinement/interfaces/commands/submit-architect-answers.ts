import type { Command } from "@system/bus/command-bus.js";

export interface SubmitArchitectAnswersCommand extends Command {
  readonly type: "submitArchitectAnswers";
  readonly featureId: string;
  readonly revisionId: string;
  readonly answers: string;
}

export interface SubmitArchitectAnswersResult {
  readonly accepted: boolean;
}
