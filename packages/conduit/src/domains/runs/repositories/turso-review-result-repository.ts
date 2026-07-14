import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";
import type { RunsDatabase } from "../interfaces/database-schema.js";
import type {
  PersistedReviewResult,
  ReviewResultRepository,
} from "../interfaces/review-result-repository.js";

function toReview(row: RunsDatabase["review_results"]): PersistedReviewResult {
  return {
    reviewId: row.review_id,
    runId: row.run_id,
    featureId: row.feature_id,
    decision: row.decision === "approved" ? "approved" : "rejected",
    findings: JSON.parse(row.findings_json),
    evidencePaths: JSON.parse(row.evidence_paths_json),
    followUp: row.follow_up ?? undefined,
    reviewedAt: row.reviewed_at,
  };
}

export class TursoReviewResultRepository implements ReviewResultRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RunsDatabase>(connection);
  }

  async save(input: PersistedReviewResult): Promise<void> {
    const result = redactPersistedValue(input);
    const values: RunsDatabase["review_results"] = {
      run_id: result.runId,
      review_id: result.reviewId,
      feature_id: result.featureId,
      decision: result.decision,
      findings_json: JSON.stringify(result.findings),
      evidence_paths_json: JSON.stringify(result.evidencePaths),
      follow_up: result.followUp ?? null,
      reviewed_at: result.reviewedAt,
    };
    await this.database
      .insertInto("review_results")
      .values(values)
      .onConflict((conflict) => conflict.column("run_id").doUpdateSet(values))
      .execute();
  }

  async load(runId: string): Promise<PersistedReviewResult | undefined> {
    const row = await this.database
      .selectFrom("review_results")
      .selectAll()
      .where("run_id", "=", runId)
      .executeTakeFirst();
    return row ? toReview(row) : undefined;
  }
}
