import type { MigrationDefinition } from "@system/storage/types/migration.js";

export const harnessRuntimeMigration = {
  id: "0007_harness_runtime",
  domain: "runs",
  scope: "project",
  checksumSource:
    "feature-packages-v1;harness-sessions-v1;harness-turns-v1;clarification-questions-v1;runtime-events-v1;result-records-v1;diagnostic-artifacts-v1",
  async up(database) {
    await database.schema
      .createTable("feature_package_versions")
      .ifNotExists()
      .addColumn("package_version_id", "text", (column) => column.primaryKey())
      .addColumn("feature_id", "text", (column) => column.notNull())
      .addColumn("package_hash", "text", (column) => column.notNull().unique())
      .addColumn("inputs_json", "text", (column) => column.notNull())
      .addColumn("created_at", "text", (column) => column.notNull())
      .execute();
    await database.schema
      .createTable("harness_sessions")
      .ifNotExists()
      .addColumn("session_id", "text", (column) => column.primaryKey())
      .addColumn("feature_id", "text", (column) => column.notNull())
      .addColumn("package_version_id", "text", (column) => column.notNull())
      .addColumn("provider_id", "text", (column) => column.notNull())
      .addColumn("harness", "text", (column) => column.notNull())
      .addColumn("harness_version", "text")
      .addColumn("protocol", "text", (column) => column.notNull())
      .addColumn("model", "text")
      .addColumn("native_session_id", "text")
      .addColumn("status", "text", (column) => column.notNull())
      .addColumn("supersedes_session_id", "text")
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
    await database.schema
      .createTable("harness_turns")
      .ifNotExists()
      .addColumn("turn_id", "text", (column) => column.primaryKey())
      .addColumn("session_id", "text", (column) => column.notNull())
      .addColumn("assignment_id", "text", (column) => column.notNull())
      .addColumn("kind", "text", (column) => column.notNull())
      .addColumn("status", "text", (column) => column.notNull())
      .addColumn("started_at", "text", (column) => column.notNull())
      .addColumn("completed_at", "text")
      .execute();
    await database.schema
      .createTable("clarification_questions")
      .ifNotExists()
      .addColumn("question_id", "text", (column) => column.primaryKey())
      .addColumn("feature_id", "text", (column) => column.notNull())
      .addColumn("revision_id", "text", (column) => column.notNull())
      .addColumn("fingerprint", "text", (column) => column.notNull())
      .addColumn("question_json", "text", (column) => column.notNull())
      .addColumn("answer", "text")
      .addColumn("repeat_count", "integer", (column) =>
        column.notNull().defaultTo(0),
      )
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("answered_at", "text")
      .addUniqueConstraint("clarification_lineage_fingerprint", [
        "feature_id",
        "revision_id",
        "fingerprint",
      ])
      .execute();
    await database.schema
      .createTable("runtime_events")
      .ifNotExists()
      .addColumn("id", "integer", (column) =>
        column.primaryKey().autoIncrement(),
      )
      .addColumn("run_id", "text", (column) => column.notNull())
      .addColumn("role_id", "text", (column) => column.notNull())
      .addColumn("sequence", "integer", (column) => column.notNull())
      .addColumn("event_json", "text", (column) => column.notNull())
      .addColumn("received_at", "text", (column) => column.notNull())
      .addUniqueConstraint("runtime_events_role_sequence", [
        "run_id",
        "role_id",
        "sequence",
      ])
      .execute();
    await database.schema
      .createTable("result_records")
      .ifNotExists()
      .addColumn("run_id", "text", (column) => column.notNull())
      .addColumn("role_id", "text", (column) => column.notNull())
      .addColumn("record_json", "text", (column) => column.notNull())
      .addColumn("received_at", "text", (column) => column.notNull())
      .addPrimaryKeyConstraint("result_records_pk", ["run_id", "role_id"])
      .execute();
    await database.schema
      .createTable("diagnostic_artifacts")
      .ifNotExists()
      .addColumn("artifact_id", "text", (column) => column.primaryKey())
      .addColumn("run_id", "text")
      .addColumn("role_id", "text")
      .addColumn("kind", "text", (column) => column.notNull())
      .addColumn("path", "text", (column) => column.notNull())
      .addColumn("size_bytes", "integer", (column) => column.notNull())
      .addColumn("truncated", "integer", (column) =>
        column.notNull().defaultTo(0),
      )
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("expires_at", "text", (column) => column.notNull())
      .execute();
  },
} satisfies MigrationDefinition;
