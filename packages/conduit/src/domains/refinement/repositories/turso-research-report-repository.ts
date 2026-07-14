import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import type { RefinementDatabase } from "../interfaces/database-schema.js";
import type { ResearchReportRepository } from "../interfaces/research-report-repository.js";
import type { ResearchReport } from "../types/research-report.js";

function toReport(row: RefinementDatabase["research_reports"]): ResearchReport {
  return {
    featureId: row.feature_id,
    report: row.report,
    updatedAt: row.updated_at,
    version: row.version,
  };
}

export class TursoResearchReportRepository implements ResearchReportRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RefinementDatabase>(connection);
  }

  async save(featureId: string, input: string): Promise<ResearchReport> {
    const existing = await this.load(featureId);
    const report = redactSecrets(input);
    const updatedAt = new Date().toISOString();
    const version = (existing?.version ?? 0) + 1;
    await this.database
      .insertInto("research_reports")
      .values({
        feature_id: featureId,
        report,
        updated_at: updatedAt,
        version,
      })
      .onConflict((conflict) =>
        conflict.column("feature_id").doUpdateSet({
          report,
          updated_at: updatedAt,
          version,
        }),
      )
      .execute();
    return { featureId, report, updatedAt, version };
  }

  async load(featureId: string): Promise<ResearchReport | undefined> {
    const row = await this.database
      .selectFrom("research_reports")
      .selectAll()
      .where("feature_id", "=", featureId)
      .executeTakeFirst();
    return row ? toReport(row) : undefined;
  }
}
