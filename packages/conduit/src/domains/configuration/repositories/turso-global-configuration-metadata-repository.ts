import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseFactory } from "@system/storage/interfaces/factory.js";
import type { ConfigurationDatabase } from "../interfaces/database-schema.js";
import type { GlobalConfigurationMetadataRepository } from "../interfaces/global-configuration-metadata-repository.js";

export class TursoGlobalConfigurationMetadataRepository implements GlobalConfigurationMetadataRepository {
  constructor(private readonly factory: DatabaseFactory) {}

  async set(key: string, value: unknown): Promise<void> {
    const connection = await this.factory.open();
    const database = createTursoKysely<ConfigurationDatabase>(connection);
    try {
      const values: ConfigurationDatabase["global_configuration_metadata"] = {
        key,
        value_json: JSON.stringify(value),
        updated_at: new Date().toISOString(),
      };
      await database
        .insertInto("global_configuration_metadata")
        .values(values)
        .onConflict((conflict) => conflict.column("key").doUpdateSet(values))
        .execute();
    } finally {
      await database.destroy();
      await connection.close();
    }
  }

  async get(key: string): Promise<unknown | undefined> {
    const connection = await this.factory.open();
    const database = createTursoKysely<ConfigurationDatabase>(connection);
    try {
      const row = await database
        .selectFrom("global_configuration_metadata")
        .select("value_json")
        .where("key", "=", key)
        .executeTakeFirst();
      return row ? JSON.parse(row.value_json) : undefined;
    } finally {
      await database.destroy();
      await connection.close();
    }
  }
}
