import type { MigrationDefinition } from "@system/storage/types/migration.js";

export const refinementStateMigration = {
  id: "0003_refinement_state",
  domain: "refinement",
  scope: "project",
  checksumSource:
    "refinement-drafts-v1;refinement-revisions-v1;refinement-events-v1;research-reports-v1",
  async up(database) {
    await database.schema
      .createTable("refinement_drafts")
      .ifNotExists()
      .addColumn("feature_id", "text", (column) => column.primaryKey())
      .addColumn("story", "text", (column) => column.notNull())
      .addColumn("test_cases", "text", (column) => column.notNull())
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .addColumn("version", "integer", (column) =>
        column.notNull().defaultTo(1),
      )
      .execute();
    await database.schema
      .createTable("refinement_revisions")
      .ifNotExists()
      .addColumn("feature_id", "text", (column) => column.notNull())
      .addColumn("revision_id", "text", (column) => column.notNull())
      .addColumn("status", "text", (column) => column.notNull())
      .addColumn("directory", "text", (column) => column.notNull())
      .addColumn("feedback", "text")
      .addColumn("questions_source", "text")
      .addColumn("answers", "text")
      .addColumn("review_decision", "text")
      .addColumn("review_feedback", "text")
      .addColumn("transcript", "text")
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .addColumn("version", "integer", (column) =>
        column.notNull().defaultTo(1),
      )
      .addPrimaryKeyConstraint("refinement_revisions_pk", [
        "feature_id",
        "revision_id",
      ])
      .execute();
    await database.schema
      .createTable("refinement_events")
      .ifNotExists()
      .addColumn("id", "integer", (column) =>
        column.primaryKey().autoIncrement(),
      )
      .addColumn("feature_id", "text", (column) => column.notNull())
      .addColumn("sequence", "integer", (column) => column.notNull())
      .addColumn("event_type", "text", (column) => column.notNull())
      .addColumn("timestamp", "text", (column) => column.notNull())
      .addColumn("content", "text", (column) => column.notNull())
      .addColumn("files_json", "text")
      .addColumn("diff", "text")
      .addUniqueConstraint("refinement_events_sequence", [
        "feature_id",
        "sequence",
      ])
      .execute();
    await database.schema
      .createTable("research_reports")
      .ifNotExists()
      .addColumn("feature_id", "text", (column) => column.primaryKey())
      .addColumn("report", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .addColumn("version", "integer", (column) =>
        column.notNull().defaultTo(1),
      )
      .execute();
  },
} satisfies MigrationDefinition;
