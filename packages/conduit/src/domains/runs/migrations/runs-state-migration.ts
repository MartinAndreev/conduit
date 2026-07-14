import type { MigrationDefinition } from "@system/storage/types/migration.js";

export const runsStateMigration = {
  id: "0004_runs_state",
  domain: "runs",
  scope: "project",
  checksumSource:
    "run-events-v1;review-results-v1;run-snapshots-v1;run-recovery-v1",
  async up(database) {
    await database.schema
      .createTable("run_events")
      .ifNotExists()
      .addColumn("id", "integer", (column) =>
        column.primaryKey().autoIncrement(),
      )
      .addColumn("run_id", "text", (column) => column.notNull())
      .addColumn("role_id", "text", (column) => column.notNull())
      .addColumn("sequence", "integer", (column) => column.notNull())
      .addColumn("event_type", "text", (column) => column.notNull())
      .addColumn("timestamp", "text", (column) => column.notNull())
      .addColumn("payload_json", "text", (column) => column.notNull())
      .addUniqueConstraint("run_events_sequence", ["run_id", "sequence"])
      .execute();
    await database.schema
      .createIndex("idx_run_events_role")
      .ifNotExists()
      .on("run_events")
      .columns(["run_id", "role_id", "sequence"])
      .execute();
    await database.schema
      .createTable("review_results")
      .ifNotExists()
      .addColumn("run_id", "text", (column) => column.primaryKey())
      .addColumn("review_id", "text", (column) => column.notNull())
      .addColumn("feature_id", "text", (column) => column.notNull())
      .addColumn("decision", "text", (column) => column.notNull())
      .addColumn("findings_json", "text", (column) => column.notNull())
      .addColumn("evidence_paths_json", "text", (column) => column.notNull())
      .addColumn("follow_up", "text")
      .addColumn("reviewed_at", "text", (column) => column.notNull())
      .execute();
    await database.schema
      .createTable("run_snapshots")
      .ifNotExists()
      .addColumn("run_id", "text", (column) => column.primaryKey())
      .addColumn("snapshot_json", "text", (column) => column.notNull())
      .addColumn("status", "text", (column) => column.notNull())
      .addColumn("version", "integer", (column) =>
        column.notNull().defaultTo(1),
      )
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
    await database.schema
      .createTable("run_recovery")
      .ifNotExists()
      .addColumn("run_id", "text", (column) => column.primaryKey())
      .addColumn("state", "text", (column) => column.notNull())
      .addColumn("diagnostic", "text")
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
  },
} satisfies MigrationDefinition;
