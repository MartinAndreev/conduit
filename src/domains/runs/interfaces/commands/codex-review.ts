import type { Command } from "../../../../system/bus/command-bus.js";

export interface CodexReviewCommand extends Command {
  readonly type: "codexReview";
  readonly projectRoot: string;
  readonly featureId: string;
  readonly runId: string;
}

export interface CodexReviewResult {
  readonly reviewId: string;
  readonly decision: "approved" | "rejected";
  readonly findingsCount: number;
  readonly evidencePaths: readonly string[];
  readonly followUp: string | undefined;
}
