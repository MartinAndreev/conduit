import type { MigrationDefinition } from "@system/storage/types/migration.js";

export const globalProfileMigration = {
  id: "0002_configuration_global_profiles",
  domain: "configuration",
  scope: "global",
  checksumSource: "global-profiles-v1;global-configuration-metadata-v1",
  async up(database) {
    await database.schema
      .createTable("global_profiles")
      .ifNotExists()
      .addColumn("name", "text", (column) => column.primaryKey())
      .addColumn("runner", "text")
      .addColumn("model", "text")
      .addColumn("effort", "text")
      .addColumn("mode", "text")
      .addColumn("read_only", "integer")
      .addColumn("owns_json", "text", (column) =>
        column.notNull().defaultTo("[]"),
      )
      .addColumn("skill_source", "text")
      .addColumn("metadata_json", "text", (column) =>
        column.notNull().defaultTo("{}"),
      )
      .addColumn("version", "integer", (column) =>
        column.notNull().defaultTo(1),
      )
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
    await database.schema
      .createTable("global_configuration_metadata")
      .ifNotExists()
      .addColumn("key", "text", (column) => column.primaryKey())
      .addColumn("value_json", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
  },
} satisfies MigrationDefinition;
