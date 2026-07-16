import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import type { Feature } from "@domains/features/types/feature.js";
import type { RefinementDatabase } from "../interfaces/database-schema.js";
import type { RefinementRevisionRepository } from "../interfaces/revision-repository.js";
import type {
  ClarificationQuestion,
  RefinementRevision,
  RevisionStatus,
} from "../types/revision.js";
import { parseQuestions } from "../helpers/question-parser.js";
import { extractArchitectEvents } from "../helpers/architect-event-parser.js";

function toRevision(
  row: RefinementDatabase["refinement_revisions"],
): RefinementRevision {
  return {
    featureId: row.feature_id,
    id: row.revision_id,
    status: row.status as RevisionStatus,
    directory: row.directory,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    ...(row.feedback ? { feedback: row.feedback } : {}),
    version: row.version,
  };
}

function featureIdOf(revision: RefinementRevision): string {
  if (!revision.featureId)
    throw new Error("A persisted refinement revision requires a feature ID.");
  return revision.featureId;
}

export class TursoRefinementRevisionRepository implements RefinementRevisionRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RefinementDatabase>(connection);
  }

  async create(
    feature: Feature,
    feedback?: string,
  ): Promise<RefinementRevision> {
    const latest = await this.getLatest(feature);
    const id = String(Number(latest?.id ?? 0) + 1).padStart(3, "0");
    const now = new Date().toISOString();
    const sanitizedFeedback = feedback?.trim()
      ? redactSecrets(feedback.trim())
      : undefined;
    await this.database
      .insertInto("refinement_revisions")
      .values({
        feature_id: feature.id,
        revision_id: id,
        status: "running",
        directory: feature.directory,
        feedback: sanitizedFeedback ?? null,
        questions_source: null,
        answers: null,
        review_decision: null,
        review_feedback: null,
        transcript: null,
        created_at: now,
        updated_at: now,
        version: 1,
      })
      .execute();
    return {
      featureId: feature.id,
      id,
      status: "running",
      directory: feature.directory,
      createdAt: now,
      updatedAt: now,
      ...(sanitizedFeedback ? { feedback: sanitizedFeedback } : {}),
      version: 1,
    };
  }

  async getLatest(feature: Feature): Promise<RefinementRevision | null> {
    const row = await this.database
      .selectFrom("refinement_revisions")
      .selectAll()
      .where("feature_id", "=", feature.id)
      .orderBy("revision_id", "desc")
      .executeTakeFirst();
    return row ? toRevision(row) : null;
  }

  async updateStatus(
    revision: RefinementRevision,
    status: RevisionStatus,
  ): Promise<RefinementRevision> {
    const updatedAt = new Date().toISOString();
    const version = revision.version ?? 1;
    const result = await this.database
      .updateTable("refinement_revisions")
      .set({ status, updated_at: updatedAt, version: version + 1 })
      .where("feature_id", "=", featureIdOf(revision))
      .where("revision_id", "=", revision.id)
      .where("version", "=", version)
      .executeTakeFirst();
    if (result.numUpdatedRows !== 1n)
      throw new Error(
        `Revision ${revision.id} was updated by another operation.`,
      );
    return { ...revision, status, updatedAt, version: version + 1 };
  }

  async saveQuestions(
    revision: RefinementRevision,
    source: string,
  ): Promise<readonly ClarificationQuestion[]> {
    const sanitized = redactSecrets(source.trim());
    await this.setText(revision, "questions", sanitized);
    return parseQuestions(sanitized);
  }

  async readQuestions(
    revision: RefinementRevision,
  ): Promise<readonly ClarificationQuestion[]> {
    const row = await this.database
      .selectFrom("refinement_revisions")
      .select("questions_source")
      .where("feature_id", "=", featureIdOf(revision))
      .where("revision_id", "=", revision.id)
      .executeTakeFirst();
    return parseQuestions(row?.questions_source ?? "");
  }

  async saveAnswers(
    revision: RefinementRevision,
    answers: string,
  ): Promise<void> {
    await this.setText(revision, "answers", redactSecrets(answers.trim()));
  }

  async readAnswers(revision: RefinementRevision): Promise<string> {
    const row = await this.database
      .selectFrom("refinement_revisions")
      .select("answers")
      .where("feature_id", "=", featureIdOf(revision))
      .where("revision_id", "=", revision.id)
      .executeTakeFirst();
    return row?.answers ?? "";
  }

  async recordReview(
    revision: RefinementRevision,
    decision: "approved" | "changes_requested",
    feedback?: string,
  ): Promise<void> {
    await this.database
      .updateTable("refinement_revisions")
      .set({
        review_decision: decision,
        review_feedback: feedback?.trim()
          ? redactSecrets(feedback.trim())
          : null,
        updated_at: new Date().toISOString(),
      })
      .where("feature_id", "=", featureIdOf(revision))
      .where("revision_id", "=", revision.id)
      .execute();
  }

  async recordRun(
    revision: RefinementRevision,
    transcript: string,
  ): Promise<void> {
    const sanitized = redactSecrets(transcript.trim());
    const events = extractArchitectEvents(sanitized);
    await this.database.transaction().execute(async (transaction) => {
      await transaction
        .updateTable("refinement_revisions")
        .set({ transcript: null, updated_at: new Date().toISOString() })
        .where("feature_id", "=", featureIdOf(revision))
        .where("revision_id", "=", revision.id)
        .execute();
      let latest = await transaction
        .selectFrom("refinement_events")
        .select((expression) =>
          expression.fn.max<number>("sequence").as("sequence"),
        )
        .where("feature_id", "=", featureIdOf(revision))
        .executeTakeFirst();
      for (const event of events) {
        const sequence = (latest?.sequence ?? 0) + 1;
        await transaction
          .insertInto("refinement_events")
          .values({
            feature_id: featureIdOf(revision),
            sequence,
            event_type: event.type,
            timestamp: event.timestamp,
            content: event.content.slice(0, 2_000),
            files_json: event.files ? JSON.stringify(event.files) : null,
            diff: event.diff?.slice(0, 256_000) ?? null,
          })
          .execute();
        latest = { sequence };
      }
    });
  }

  private async setText(
    revision: RefinementRevision,
    field: "questions" | "answers",
    value: string,
  ): Promise<void> {
    const update =
      field === "questions"
        ? { questions_source: value, updated_at: new Date().toISOString() }
        : { answers: value, updated_at: new Date().toISOString() };
    await this.database
      .updateTable("refinement_revisions")
      .set(update)
      .where("feature_id", "=", featureIdOf(revision))
      .where("revision_id", "=", revision.id)
      .execute();
  }
}
