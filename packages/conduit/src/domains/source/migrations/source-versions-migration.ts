import type { MigrationDefinition } from "@system/storage/types/migration.js";

export const sourceVersionsMigration = {
  id: "0006_source_versions_primitives",
  domain: "source",
  scope: "project",
  checksumSource: "source-versions-v1",
  async up(database) {
    await database.schema
      .createTable("source_versions")
      .ifNotExists()
      .addColumn("source_path", "text", (column) => column.notNull())
      .addColumn("source_version", "text", (column) => column.notNull())
      .addColumn("content_checksum", "text", (column) => column.notNull())
      .addColumn("observed_at", "text", (column) => column.notNull())
      .addColumn("metadata_json", "text", (column) =>
        column.notNull().defaultTo("{}"),
      )
      .addPrimaryKeyConstraint("source_versions_pk", [
        "source_path",
        "source_version",
      ])
      .execute();
  },
} satisfies MigrationDefinition;
