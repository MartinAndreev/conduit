import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  ReviewRunCommand,
  ReviewRunResult,
} from "../interfaces/commands/review-run.js";
import type { ReviewResultRepository } from "../interfaces/review-result-repository.js";

export function createReviewRunHandler(
  reviewRepository: ReviewResultRepository,
): CommandHandler<ReviewRunCommand, ReviewRunResult> {
  return async (command) => {
    try {
      const reviewId = `review-${command.runId}-${Date.now()}`;
      const evidencePaths = command.findings
        .filter((f) => f.file)
        .map((f) => f.file!);

      await reviewRepository.save({
        reviewId,
        runId: command.runId,
        featureId: command.featureId,
        decision: command.decision,
        findings: command.findings,
        evidencePaths,
        followUp: command.followUp,
        reviewedAt: new Date().toISOString(),
      });

      return {
        success: true,
        data: {
          reviewId,
          decision: command.decision,
          findingsCount: command.findings.length,
          evidencePaths,
          followUp: command.followUp,
        },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "REVIEW_SAVE_ERROR",
          message: `Failed to save review: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
