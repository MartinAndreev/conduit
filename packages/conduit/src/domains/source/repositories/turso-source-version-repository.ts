import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";
import type { SourceDatabase } from "../interfaces/database-schema.js";
import type { SourceVersionRepository } from "../interfaces/source-version-repository.js";
import type { SourceVersion } from "../types/source-version.js";

function toSourceVersion(
  row: SourceDatabase["source_versions"],
): SourceVersion {
  return {
    sourcePath: row.source_path,
    sourceVersion: row.source_version,
    contentChecksum: row.content_checksum,
    observedAt: row.observed_at,
    metadata: JSON.parse(row.metadata_json),
  };
}

export class TursoSourceVersionRepository implements SourceVersionRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<SourceDatabase>(connection);
  }

  async save(input: SourceVersion): Promise<void> {
    const version = redactPersistedValue(input);
    const values: SourceDatabase["source_versions"] = {
      source_path: version.sourcePath,
      source_version: version.sourceVersion,
      content_checksum: version.contentChecksum,
      observed_at: version.observedAt,
      metadata_json: JSON.stringify(version.metadata),
    };
    await this.database
      .insertInto("source_versions")
      .values(values)
      .onConflict((conflict) =>
        conflict.columns(["source_path", "source_version"]).doUpdateSet(values),
      )
      .execute();
  }

  async load(
    sourcePath: string,
    sourceVersion: string,
  ): Promise<SourceVersion | undefined> {
    const row = await this.database
      .selectFrom("source_versions")
      .selectAll()
      .where("source_path", "=", sourcePath)
      .where("source_version", "=", sourceVersion)
      .executeTakeFirst();
    return row ? toSourceVersion(row) : undefined;
  }

  async listBySource(sourcePath: string): Promise<readonly SourceVersion[]> {
    return (
      await this.database
        .selectFrom("source_versions")
        .selectAll()
        .where("source_path", "=", sourcePath)
        .orderBy("observed_at", "desc")
        .execute()
    ).map(toSourceVersion);
  }
}
