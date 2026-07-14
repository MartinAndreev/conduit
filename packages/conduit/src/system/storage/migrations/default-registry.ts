import { globalProfileMigration } from "../../../domains/configuration/migrations/global-profile-migration.js";
import { refinementStateMigration } from "../../../domains/refinement/migrations/refinement-state-migration.js";
import { runsStateMigration } from "../../../domains/runs/migrations/runs-state-migration.js";
import { sourceVersionsMigration } from "../../../domains/source/migrations/source-versions-migration.js";
import { OrderedMigrationRegistry } from "./migration-registry.js";
import { systemProjectMigration } from "./system-migrations.js";

export function createDefaultMigrationRegistry(): OrderedMigrationRegistry {
  const registry = new OrderedMigrationRegistry();
  registry.register(systemProjectMigration);
  registry.register(globalProfileMigration);
  registry.register(refinementStateMigration);
  registry.register(runsStateMigration);
  registry.register(sourceVersionsMigration);
  return registry;
}
