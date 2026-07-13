import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetReviewResultQuery,
  GetReviewResultReadModel,
} from "../interfaces/queries/get-review-result.js";
import type { ReviewResultRepository } from "../interfaces/review-result-repository.js";

export function createGetReviewResultHandler(
  repository: ReviewResultRepository,
): QueryHandler<GetReviewResultQuery, GetReviewResultReadModel> {
  return async (query) => {
    try {
      const result = await repository.load(query.runId);
      if (!result) {
        return {
          success: true,
          data: {
            review: {
              reviewId: undefined,
              decision: undefined,
              findings: [],
              evidencePaths: [],
              followUp: undefined,
              reviewedAt: undefined,
            },
          },
        };
      }
      return {
        success: true,
        data: {
          review: {
            reviewId: result.reviewId,
            decision: result.decision,
            findings: result.findings,
            evidencePaths: result.evidencePaths,
            followUp: result.followUp,
            reviewedAt: result.reviewedAt,
          },
        },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "REVIEW_LOAD_ERROR",
          message: `Failed to load review: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
