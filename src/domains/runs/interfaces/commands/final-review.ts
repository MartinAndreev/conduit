import type { Command } from "../../../../system/bus/command-bus.js";

export interface FinalReviewCommand extends Command {
  readonly type: "finalReview";
  readonly projectRoot: string;
  readonly featureId: string;
  readonly runId: string;
}

export interface FinalReviewResult {
  readonly reviewId: string;
  readonly decision: "approved" | "rejected";
  readonly findingsCount: number;
  readonly evidencePaths: readonly string[];
  readonly followUp: string | undefined;
}
