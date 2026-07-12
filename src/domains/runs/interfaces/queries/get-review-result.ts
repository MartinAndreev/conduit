import type { Query } from "../../../../system/bus/query-bus.js";
import type { ReviewDecision, ReviewFinding } from "../../types/review.js";

export interface GetReviewResultQuery extends Query {
  readonly type: "getReviewResult";
  readonly runId: string;
}

export interface ReviewResultReadModel {
  readonly reviewId: string | undefined;
  readonly decision: ReviewDecision | undefined;
  readonly findings: readonly ReviewFinding[];
  readonly evidencePaths: readonly string[];
  readonly followUp: string | undefined;
  readonly reviewedAt: string | undefined;
}

export interface GetReviewResultReadModel {
  readonly review: ReviewResultReadModel;
}
