import type { MigrationDefinition } from "../types/migration.js";

export const systemProjectMigration = {
  id: "0001_system_project_metadata",
  domain: "system",
  scope: "project",
  checksumSource: "migration-recovery-v1;import-ledger-v1",
  async up(database) {
    await database.schema
      .createTable("migration_recovery")
      .ifNotExists()
      .addColumn("key", "text", (column) => column.primaryKey())
      .addColumn("value", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
    await database.schema
      .createTable("import_ledger")
      .ifNotExists()
      .addColumn("source_path", "text", (column) => column.primaryKey())
      .addColumn("source_checksum", "text", (column) => column.notNull())
      .addColumn("imported_at", "text", (column) => column.notNull())
      .addColumn("record_count", "integer", (column) => column.notNull())
      .addColumn("status", "text", (column) => column.notNull())
      .addColumn("diagnostic", "text")
      .execute();
  },
} satisfies MigrationDefinition;
