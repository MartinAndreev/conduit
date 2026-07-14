import { createHash } from "node:crypto";
import { mkdir } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { createTursoKysely } from "../adapters/kysely-turso-dialect.js";
import {
  redactStorageDiagnostic,
  StorageError,
  toStorageError,
} from "../errors/storage-error.js";
import type { DatabaseConnection } from "../interfaces/database.js";
import type { MigrationDatabase } from "../interfaces/migration-database.js";
import type {
  MigrationRegistry,
  MigrationRunner,
} from "../interfaces/migration.js";
import type { DatabaseScope } from "../types/database.js";
import type {
  MigrationDefinition,
  MigrationResult,
} from "../types/migration.js";
import { TursoMigrationHistoryRepository } from "./migration-history-repository.js";
import { verifyBackupContainsNoSecrets } from "../security/backup-redaction.js";

const migrationLocks = new Map<string, Promise<void>>();

export function migrationChecksum(migration: MigrationDefinition): string {
  return createHash("sha256")
    .update(
      JSON.stringify({
        id: migration.id,
        domain: migration.domain,
        scope: migration.scope,
        checksumSource: migration.checksumSource,
      }),
    )
    .digest("hex");
}

function backupName(databasePath: string): string {
  const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
  return `${basename(databasePath, ".db")}-${timestamp}.db`;
}

export class DefaultMigrationRunner implements MigrationRunner {
  constructor(private readonly registry: MigrationRegistry) {}

  async migrate(
    connection: DatabaseConnection,
    scope: DatabaseScope,
  ): Promise<MigrationResult> {
    const previous =
      migrationLocks.get(connection.databasePath) ?? Promise.resolve();
    let release: (() => void) | undefined;
    const current = new Promise<void>((resolve) => {
      release = resolve;
    });
    migrationLocks.set(connection.databasePath, current);
    await previous;
    try {
      try {
        return await this.run(connection, scope);
      } catch (error) {
        if (error instanceof StorageError) throw error;
        throw toStorageError({
          scope,
          operation: "run database migrations",
          cause: error,
          remediation:
            "Stop Conduit, preserve the database file, and restore the newest backup after validating the file is corrupt.",
        });
      }
    } finally {
      release?.();
      if (migrationLocks.get(connection.databasePath) === current)
        migrationLocks.delete(connection.databasePath);
    }
  }

  private async run(
    connection: DatabaseConnection,
    scope: DatabaseScope,
  ): Promise<MigrationResult> {
    const database = createTursoKysely<MigrationDatabase>(connection);
    const history = new TursoMigrationHistoryRepository(database);
    await history.initialize();
    const records = await history.loadAll();
    const recovered = records
      .filter((record) => record.status === "running")
      .map((record) => record.id);
    const applied = new Map(records.map((record) => [record.id, record]));
    const definitions = this.registry.list(scope);
    const pending: Array<{
      migration: MigrationDefinition;
      checksum: string;
    }> = [];

    for (const migration of definitions) {
      const checksum = migrationChecksum(migration);
      const existing = applied.get(migration.id);
      if (existing?.status === "succeeded") {
        if (existing.checksum !== checksum) {
          throw new StorageError({
            scope,
            operation: "verify migration checksum",
            message: `Migration ${migration.id} checksum does not match the applied migration.`,
            remediation:
              "Restore the original migration or restore the database from a known-good backup.",
          });
        }
        continue;
      }
      pending.push({ migration, checksum });
    }

    if (!pending.length) return { applied: [], recovered };

    const backupsDirectory = join(dirname(connection.databasePath), "backups");
    await mkdir(backupsDirectory, { recursive: true, mode: 0o700 });
    const backupPath = join(
      backupsDirectory,
      backupName(connection.databasePath),
    );
    await connection.checkpoint();
    await connection.backup(backupPath);
    await verifyBackupContainsNoSecrets(backupPath);
    await database
      .insertInto("migration_recovery")
      .values({
        key: `${scope}:latest-backup`,
        value: JSON.stringify({
          backupPath,
          pending: pending.map(({ migration }) => migration.id),
        }),
        updated_at: new Date().toISOString(),
      })
      .onConflict((conflict) =>
        conflict.column("key").doUpdateSet({
          value: JSON.stringify({
            backupPath,
            pending: pending.map(({ migration }) => migration.id),
          }),
          updated_at: new Date().toISOString(),
        }),
      )
      .execute();

    const completed: string[] = [];
    for (const { migration, checksum } of pending) {
      await history.recordRunning(migration, checksum);
      const started = performance.now();
      try {
        await database.transaction().execute(async (transaction) => {
          await migration.up(transaction);
        });
        await history.recordFinished(
          migration,
          checksum,
          Math.max(0, Math.round(performance.now() - started)),
          true,
        );
        completed.push(migration.id);
      } catch (error) {
        await history
          .recordFinished(
            migration,
            checksum,
            Math.max(0, Math.round(performance.now() - started)),
            false,
          )
          .catch(() => {});
        throw new StorageError({
          scope,
          operation: `apply migration ${migration.id}`,
          message: `Migration ${migration.id} failed.`,
          remediation: `Inspect the sanitized diagnostic and restore ${backupPath} if retry is unsafe.`,
          cause: new Error(redactStorageDiagnostic(error)),
        });
      }
    }
    return { applied: completed, recovered, backupPath };
  }
}
