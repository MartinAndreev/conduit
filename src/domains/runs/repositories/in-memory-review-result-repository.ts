import type {
  PersistedReviewResult,
  ReviewResultRepository,
} from "../interfaces/review-result-repository.js";

export class InMemoryReviewResultRepository implements ReviewResultRepository {
  private readonly results = new Map<string, PersistedReviewResult>();

  async save(result: PersistedReviewResult): Promise<void> {
    this.results.set(result.runId, result);
  }

  async load(runId: string): Promise<PersistedReviewResult | undefined> {
    return this.results.get(runId);
  }
}
