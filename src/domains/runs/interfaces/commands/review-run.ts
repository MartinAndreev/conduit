import type { Command } from "../../../../system/bus/command-bus.js";
import type { ReviewDecision, ReviewFinding } from "../../types/review.js";

export interface ReviewRunCommand extends Command {
  readonly type: "reviewRun";
  readonly projectRoot: string;
  readonly featureId: string;
  readonly runId: string;
  readonly decision: ReviewDecision;
  readonly findings: readonly ReviewFinding[];
  readonly followUp?: string;
}

export interface ReviewRunResult {
  readonly reviewId: string;
  readonly decision: ReviewDecision;
  readonly findingsCount: number;
  readonly evidencePaths: readonly string[];
  readonly followUp: string | undefined;
}
