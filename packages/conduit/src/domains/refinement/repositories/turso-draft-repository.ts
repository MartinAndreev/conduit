import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";
import { DraftVersionConflictError } from "../errors/draft-version-conflict-error.js";
import type { RefinementDatabase } from "../interfaces/database-schema.js";
import type { DraftRepository } from "../interfaces/draft-repository.js";
import type { RefinementDraft } from "../types/draft.js";

function toDraft(
  row: RefinementDatabase["refinement_drafts"],
): RefinementDraft {
  return {
    featureId: row.feature_id,
    story: row.story,
    testCases: row.test_cases,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    version: row.version,
  };
}

export class TursoDraftRepository implements DraftRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RefinementDatabase>(connection);
  }

  async save(input: RefinementDraft): Promise<string> {
    const draft = redactPersistedValue(input);
    const existing = await this.load(draft.featureId);
    if (!existing) {
      await this.database
        .insertInto("refinement_drafts")
        .values({
          feature_id: draft.featureId,
          story: draft.story,
          test_cases: draft.testCases,
          created_at: draft.createdAt,
          updated_at: draft.updatedAt,
          version: 1,
        })
        .execute();
    } else {
      const expected = draft.version ?? existing.version ?? 1;
      const result = await this.database
        .updateTable("refinement_drafts")
        .set({
          story: draft.story,
          test_cases: draft.testCases,
          updated_at: draft.updatedAt,
          version: expected + 1,
        })
        .where("feature_id", "=", draft.featureId)
        .where("version", "=", expected)
        .executeTakeFirst();
      if (result.numUpdatedRows !== 1n)
        throw new DraftVersionConflictError(draft.featureId);
    }
    return `conduit://drafts/${encodeURIComponent(draft.featureId)}`;
  }

  async load(featureId: string): Promise<RefinementDraft | null> {
    const row = await this.database
      .selectFrom("refinement_drafts")
      .selectAll()
      .where("feature_id", "=", featureId)
      .executeTakeFirst();
    return row ? toDraft(row) : null;
  }

  async discard(featureId: string): Promise<boolean> {
    const result = await this.database
      .deleteFrom("refinement_drafts")
      .where("feature_id", "=", featureId)
      .executeTakeFirst();
    return result.numDeletedRows > 0n;
  }

  async list(): Promise<readonly RefinementDraft[]> {
    return (
      await this.database
        .selectFrom("refinement_drafts")
        .selectAll()
        .orderBy("updated_at", "desc")
        .orderBy("feature_id")
        .execute()
    ).map(toDraft);
  }
}
