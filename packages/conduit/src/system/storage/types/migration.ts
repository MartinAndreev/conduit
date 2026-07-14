import type { DatabaseScope } from "./database.js";
import type { Kysely } from "kysely";

export type MigrationSchemaBuilder = Pick<Kysely<never>, "schema">;

export type MigrationStatus = "running" | "succeeded" | "failed";

export type MigrationDefinition = Readonly<{
  id: string;
  domain: string;
  scope: DatabaseScope;
  checksumSource: string;
  up(database: MigrationSchemaBuilder): Promise<void>;
  down?(database: MigrationSchemaBuilder): Promise<void>;
}>;

export type AppliedMigration = Readonly<{
  id: string;
  domain: string;
  scope: DatabaseScope;
  checksum: string;
  appliedAt: string;
  durationMs: number;
  status: MigrationStatus;
}>;

export type MigrationResult = Readonly<{
  applied: readonly string[];
  recovered: readonly string[];
  backupPath?: string;
}>;
