import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import type {
  ReviewResultRepository,
  PersistedReviewResult,
} from "../interfaces/review-result-repository.js";

export class FileReviewResultRepository implements ReviewResultRepository {
  constructor(private readonly stateDir: string) {}

  private reviewPath(runId: string): string {
    return path.join(this.stateDir, "runs", runId, "review.json");
  }

  async save(result: PersistedReviewResult): Promise<void> {
    const filePath = this.reviewPath(result.runId);
    await mkdir(path.dirname(filePath), { recursive: true });
    await writeFile(filePath, JSON.stringify(result, null, 2));
  }

  async load(runId: string): Promise<PersistedReviewResult | undefined> {
    try {
      const raw = await readFile(this.reviewPath(runId), "utf8");
      return JSON.parse(raw) as PersistedReviewResult;
    } catch {
      return undefined;
    }
  }
}
