import type { ReviewResult } from "../types/review.js";

export type { ReviewResult as PersistedReviewResult };

export interface ReviewResultRepository {
  save(result: ReviewResult): Promise<void>;
  load(runId: string): Promise<ReviewResult | undefined>;
}
