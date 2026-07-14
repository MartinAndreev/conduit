import type { MigrationRegistry } from "../interfaces/migration.js";
import type { DatabaseScope } from "../types/database.js";
import type { MigrationDefinition } from "../types/migration.js";

export class OrderedMigrationRegistry implements MigrationRegistry {
  private readonly migrations = new Map<string, MigrationDefinition>();

  register(migration: MigrationDefinition): void {
    const key = `${migration.scope}:${migration.id}`;
    if (this.migrations.has(key)) {
      throw new Error(
        `Duplicate migration ${migration.id} for ${migration.scope}`,
      );
    }
    this.migrations.set(key, migration);
  }

  list(scope: DatabaseScope): readonly MigrationDefinition[] {
    return [...this.migrations.values()]
      .filter((migration) => migration.scope === scope)
      .sort((left, right) => left.id.localeCompare(right.id));
  }
}
