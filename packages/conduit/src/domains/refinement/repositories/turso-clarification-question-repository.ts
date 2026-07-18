import { createHash } from "node:crypto";
import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import type { RefinementDatabase } from "../interfaces/database-schema.js";
import type { ClarificationQuestionRepository } from "../interfaces/clarification-question-repository.js";
import type { ClarificationQuestion } from "../types/revision.js";
import type { PersistedClarificationQuestion } from "../types/clarification.js";

function fingerprint(question: ClarificationQuestion): string {
  const canonical = JSON.stringify({
    question: question.question.trim().toLowerCase(),
    context: question.context?.trim().toLowerCase() ?? "",
    options: question.options.map((option) => option.trim().toLowerCase()),
    unblocker: question.unblocker?.trim().toLowerCase() ?? "",
  });
  return createHash("sha256").update(canonical).digest("hex");
}

function persisted(
  row: RefinementDatabase["clarification_questions"],
): PersistedClarificationQuestion {
  return {
    id: row.question_id,
    featureId: row.feature_id,
    revisionId: row.revision_id,
    fingerprint: row.fingerprint,
    question: JSON.parse(row.question_json) as ClarificationQuestion,
    ...(row.answer ? { answer: row.answer } : {}),
    repeatCount: row.repeat_count,
    createdAt: row.created_at,
    ...(row.answered_at ? { answeredAt: row.answered_at } : {}),
  };
}

export class TursoClarificationQuestionRepository implements ClarificationQuestionRepository {
  private readonly database;
  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RefinementDatabase>(connection);
  }

  async record(
    featureId: string,
    revisionId: string,
    questions: readonly ClarificationQuestion[],
  ) {
    return this.database.transaction().execute(async (transaction) => {
      const unresolved: PersistedClarificationQuestion[] = [];
      const reminders: PersistedClarificationQuestion[] = [];
      for (const input of questions) {
        const question = JSON.parse(
          redactSecrets(JSON.stringify(input)),
        ) as ClarificationQuestion;
        const hash = fingerprint(question);
        const existing = await transaction
          .selectFrom("clarification_questions")
          .selectAll()
          .where("feature_id", "=", featureId)
          .where("revision_id", "=", revisionId)
          .where("fingerprint", "=", hash)
          .executeTakeFirst();
        if (existing) {
          if (!existing.answer) {
            unresolved.push(persisted(existing));
            continue;
          }
          if (existing.repeat_count >= 1)
            throw new Error("REPEATED_CLARIFICATION_LOOP");
          await transaction
            .updateTable("clarification_questions")
            .set({ repeat_count: 1 })
            .where("question_id", "=", existing.question_id)
            .execute();
          reminders.push(persisted({ ...existing, repeat_count: 1 }));
          continue;
        }
        const now = new Date().toISOString();
        const row: RefinementDatabase["clarification_questions"] = {
          question_id: createHash("sha256")
            .update(`${featureId}:${revisionId}:${hash}`)
            .digest("hex"),
          feature_id: featureId,
          revision_id: revisionId,
          fingerprint: hash,
          question_json: JSON.stringify(question),
          answer: null,
          repeat_count: 0,
          created_at: now,
          answered_at: null,
        };
        await transaction
          .insertInto("clarification_questions")
          .values(row)
          .execute();
        unresolved.push(persisted(row));
      }
      return { unresolved, reminders };
    });
  }

  async answerUnresolved(
    featureId: string,
    revisionId: string,
    answer: string,
  ): Promise<void> {
    const sanitized = redactSecrets(answer.trim());
    if (!sanitized) throw new Error("Clarification answer cannot be empty");
    await this.database
      .updateTable("clarification_questions")
      .set({ answer: sanitized, answered_at: new Date().toISOString() })
      .where("feature_id", "=", featureId)
      .where("revision_id", "=", revisionId)
      .where("answer", "is", null)
      .execute();
  }

  async unresolved(
    featureId: string,
    revisionId: string,
  ): Promise<readonly PersistedClarificationQuestion[]> {
    const rows = await this.database
      .selectFrom("clarification_questions")
      .selectAll()
      .where("feature_id", "=", featureId)
      .where("revision_id", "=", revisionId)
      .where("answer", "is", null)
      .orderBy("created_at")
      .execute();
    return rows.map(persisted);
  }
}
