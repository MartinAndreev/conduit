import type { Kysely } from "kysely";
import type { MigrationDatabase } from "../interfaces/migration-database.js";
import type { MigrationHistoryRepository } from "../interfaces/migration.js";
import type {
  AppliedMigration,
  MigrationDefinition,
} from "../types/migration.js";

export class TursoMigrationHistoryRepository implements MigrationHistoryRepository {
  constructor(private readonly database: Kysely<MigrationDatabase>) {}

  async initialize(): Promise<void> {
    await this.database.schema
      .createTable("schema_migrations")
      .ifNotExists()
      .addColumn("id", "text", (column) => column.notNull())
      .addColumn("domain", "text", (column) => column.notNull())
      .addColumn("scope", "text", (column) => column.notNull())
      .addColumn("checksum", "text", (column) => column.notNull())
      .addColumn("applied_at", "text", (column) => column.notNull())
      .addColumn("duration_ms", "integer", (column) =>
        column.notNull().defaultTo(0),
      )
      .addColumn("status", "text", (column) => column.notNull())
      .addPrimaryKeyConstraint("schema_migrations_pk", ["scope", "id"])
      .execute();
    await this.database.schema
      .createTable("migration_recovery")
      .ifNotExists()
      .addColumn("key", "text", (column) => column.primaryKey())
      .addColumn("value", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .execute();
  }

  async loadAll(): Promise<readonly AppliedMigration[]> {
    const rows = await this.database
      .selectFrom("schema_migrations")
      .selectAll()
      .orderBy("id")
      .execute();
    return rows.map((row) => ({
      id: row.id,
      domain: row.domain,
      scope: row.scope,
      checksum: row.checksum,
      appliedAt: row.applied_at,
      durationMs: row.duration_ms,
      status: row.status,
    }));
  }

  async recordRunning(
    migration: MigrationDefinition,
    checksum: string,
  ): Promise<void> {
    const appliedAt = new Date().toISOString();
    await this.database
      .insertInto("schema_migrations")
      .values({
        id: migration.id,
        domain: migration.domain,
        scope: migration.scope,
        checksum,
        applied_at: appliedAt,
        duration_ms: 0,
        status: "running",
      })
      .onConflict((conflict) =>
        conflict.columns(["scope", "id"]).doUpdateSet({
          domain: migration.domain,
          checksum,
          applied_at: appliedAt,
          duration_ms: 0,
          status: "running",
        }),
      )
      .execute();
  }

  async recordFinished(
    migration: MigrationDefinition,
    checksum: string,
    durationMs: number,
    succeeded: boolean,
  ): Promise<void> {
    await this.database
      .updateTable("schema_migrations")
      .set({
        checksum,
        duration_ms: durationMs,
        status: succeeded ? "succeeded" : "failed",
      })
      .where("scope", "=", migration.scope)
      .where("id", "=", migration.id)
      .execute();
  }
}
