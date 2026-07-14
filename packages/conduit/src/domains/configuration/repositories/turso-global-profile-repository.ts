import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseFactory } from "@system/storage/interfaces/factory.js";
import { containsSecret } from "@system/storage/security/secret-redaction.js";
import { GlobalProfileError } from "../errors/global-profile-error.js";
import type { ConfigurationDatabase } from "../interfaces/database-schema.js";
import type { GlobalProfileRepository } from "../interfaces/global-profile-repository.js";
import type {
  GlobalProfile,
  SaveGlobalProfileInput,
} from "../types/global-profile.js";

function parseProfile(
  row: ConfigurationDatabase["global_profiles"],
): GlobalProfile {
  return {
    name: row.name,
    ...(row.runner ? { runner: row.runner } : {}),
    ...(row.model ? { model: row.model } : {}),
    ...(row.effort ? { effort: row.effort as GlobalProfile["effort"] } : {}),
    ...(row.mode ? { mode: row.mode } : {}),
    ...(row.read_only === null ? {} : { readOnly: Boolean(row.read_only) }),
    owns: JSON.parse(row.owns_json) as string[],
    ...(row.skill_source ? { skillSource: row.skill_source } : {}),
    metadata: JSON.parse(row.metadata_json) as Record<string, string>,
    version: row.version,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function assertSafe(profile: SaveGlobalProfileInput): void {
  if (containsSecret(JSON.stringify(profile))) {
    throw new GlobalProfileError(
      "SECRET_REJECTED",
      "Global profiles cannot contain credentials or secret-like values.",
    );
  }
  if (!profile.name.trim()) {
    throw new GlobalProfileError(
      "INVALID_PROFILE",
      "Profile name is required.",
    );
  }
}

export class TursoGlobalProfileRepository implements GlobalProfileRepository {
  constructor(private readonly factory: DatabaseFactory) {}

  async load(name: string): Promise<GlobalProfile | undefined> {
    const connection = await this.factory.open();
    const database = createTursoKysely<ConfigurationDatabase>(connection);
    try {
      const row = await database
        .selectFrom("global_profiles")
        .selectAll()
        .where("name", "=", name)
        .executeTakeFirst();
      return row ? parseProfile(row) : undefined;
    } finally {
      await database.destroy();
      await connection.close();
    }
  }

  async list(): Promise<readonly GlobalProfile[]> {
    const connection = await this.factory.open();
    const database = createTursoKysely<ConfigurationDatabase>(connection);
    try {
      return (
        await database
          .selectFrom("global_profiles")
          .selectAll()
          .orderBy("name")
          .execute()
      ).map(parseProfile);
    } finally {
      await database.destroy();
      await connection.close();
    }
  }

  async save(input: SaveGlobalProfileInput): Promise<GlobalProfile> {
    assertSafe(input);
    const connection = await this.factory.open();
    const database = createTursoKysely<ConfigurationDatabase>(connection);
    try {
      return await database.transaction().execute(async (transaction) => {
        const existing = await transaction
          .selectFrom("global_profiles")
          .selectAll()
          .where("name", "=", input.name)
          .executeTakeFirst();
        if (
          input.expectedVersion !== undefined &&
          existing?.version !== input.expectedVersion
        )
          throw new GlobalProfileError(
            "VERSION_CONFLICT",
            `Global profile ${input.name} was updated by another operation.`,
          );
        const now = new Date().toISOString();
        const values: ConfigurationDatabase["global_profiles"] = {
          name: input.name,
          runner: input.runner ?? existing?.runner ?? null,
          model: input.model ?? existing?.model ?? null,
          effort: input.effort ?? existing?.effort ?? null,
          mode: input.mode ?? existing?.mode ?? null,
          read_only:
            input.readOnly === undefined
              ? (existing?.read_only ?? null)
              : Number(input.readOnly),
          owns_json: JSON.stringify(
            input.owns ??
              (existing ? (JSON.parse(existing.owns_json) as string[]) : []),
          ),
          skill_source: input.skillSource ?? existing?.skill_source ?? null,
          metadata_json: JSON.stringify(
            input.metadata ??
              (existing
                ? (JSON.parse(existing.metadata_json) as Record<string, string>)
                : {}),
          ),
          version: (existing?.version ?? 0) + 1,
          created_at: existing?.created_at ?? now,
          updated_at: now,
        };
        if (!existing) {
          await transaction
            .insertInto("global_profiles")
            .values(values)
            .execute();
        } else {
          const updated = await transaction
            .updateTable("global_profiles")
            .set(values)
            .where("name", "=", input.name)
            .where("version", "=", existing.version)
            .executeTakeFirst();
          if (updated.numUpdatedRows !== 1n)
            throw new GlobalProfileError(
              "VERSION_CONFLICT",
              `Global profile ${input.name} was updated by another operation.`,
            );
        }
        return parseProfile(values);
      });
    } finally {
      await database.destroy();
      await connection.close();
    }
  }

  async delete(name: string): Promise<boolean> {
    const connection = await this.factory.open();
    const database = createTursoKysely<ConfigurationDatabase>(connection);
    try {
      const result = await database
        .deleteFrom("global_profiles")
        .where("name", "=", name)
        .executeTakeFirst();
      return result.numDeletedRows > 0n;
    } finally {
      await database.destroy();
      await connection.close();
    }
  }
}
