import { sql } from "kysely";
import type { MigrationDefinition } from "@system/storage/types/migration.js";

export const roleWorkspaceMigration = {
  id: "0008_role_workspaces",
  domain: "runs",
  scope: "project",
  checksumSource:
    "role-workspace-slots-v1;role-workspace-generations-v1;fenced-leases-v1",
  async up(database) {
    await database.schema
      .createTable("role_workspace_slots")
      .ifNotExists()
      .addColumn("repository_id", "text", (column) => column.notNull())
      .addColumn("role_key", "text", (column) => column.notNull())
      .addColumn("generation", "integer", (column) => column.notNull())
      .addColumn("workspace_path", "text", (column) =>
        column.notNull().unique(),
      )
      .addColumn("owning_run_id", "text", (column) => column.notNull())
      .addColumn("state", "text", (column) => column.notNull())
      .addColumn("starting_head", "text", (column) => column.notNull())
      .addColumn("package_hash", "text", (column) => column.notNull())
      .addColumn("assignment_hash", "text", (column) => column.notNull())
      .addColumn("worktree_head", "text")
      .addColumn("branch_name", "text", (column) => column.notNull().unique())
      .addColumn("lease_owner", "text")
      .addColumn("fencing_token", "integer", (column) => column.notNull())
      .addColumn("leased_at", "text")
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("updated_at", "text", (column) => column.notNull())
      .addCheckConstraint(
        "role_workspace_generation_positive",
        sql`generation > 0`,
      )
      .addCheckConstraint(
        "role_workspace_fencing_nonnegative",
        sql`fencing_token >= 0`,
      )
      .addPrimaryKeyConstraint("role_workspace_slots_pk", [
        "repository_id",
        "role_key",
      ])
      .execute();
    await database.schema
      .createTable("role_workspace_generations")
      .ifNotExists()
      .addColumn("repository_id", "text", (column) => column.notNull())
      .addColumn("role_key", "text", (column) => column.notNull())
      .addColumn("generation", "integer", (column) => column.notNull())
      .addColumn("workspace_path", "text", (column) => column.notNull())
      .addColumn("owning_run_id", "text", (column) => column.notNull())
      .addColumn("starting_head", "text", (column) => column.notNull())
      .addColumn("package_hash", "text", (column) => column.notNull())
      .addColumn("assignment_hash", "text", (column) => column.notNull())
      .addColumn("branch_name", "text", (column) => column.notNull())
      .addColumn("branch_oid", "text")
      .addColumn("outcome", "text")
      .addColumn("promotion_oid", "text")
      .addColumn("created_at", "text", (column) => column.notNull())
      .addColumn("completed_at", "text")
      .addCheckConstraint(
        "role_workspace_lineage_generation_positive",
        sql`generation > 0`,
      )
      .addPrimaryKeyConstraint("role_workspace_generations_pk", [
        "repository_id",
        "role_key",
        "generation",
      ])
      .execute();
  },
} satisfies MigrationDefinition;
