import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";
import type { ConduitRuntimeEvent } from "@system/communication/types/runtime-event.js";
import type { RunsDatabase } from "../interfaces/database-schema.js";
import type { RuntimeEventRepository } from "../interfaces/runtime-event-repository.js";

export class TursoRuntimeEventRepository implements RuntimeEventRepository {
  private readonly database;
  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RunsDatabase>(connection);
  }

  async append(input: ConduitRuntimeEvent): Promise<void> {
    const event = redactPersistedValue(input);
    const runId = event.context.runId;
    const roleId = event.context.roleId;
    if (!runId || !roleId)
      throw new Error(
        "A runtime event requires Conduit-owned run and role IDs",
      );
    await this.database.transaction().execute(async (transaction) => {
      const latest = await transaction
        .selectFrom("runtime_events")
        .select(({ fn }) => fn.max<number>("sequence").as("max_sequence"))
        .where("run_id", "=", runId)
        .where("role_id", "=", roleId)
        .executeTakeFirst();
      const sequence = (latest?.max_sequence ?? 0) + 1;
      const persisted = { ...event, sequence };
      await transaction
        .insertInto("runtime_events")
        .values({
          run_id: runId,
          role_id: roleId,
          sequence,
          event_json: JSON.stringify(persisted),
          received_at: persisted.receivedAt,
        })
        .execute();
    });
  }

  async loadByRole(
    runId: string,
    roleId: string,
  ): Promise<readonly ConduitRuntimeEvent[]> {
    const rows = await this.database
      .selectFrom("runtime_events")
      .select("event_json")
      .where("run_id", "=", runId)
      .where("role_id", "=", roleId)
      .orderBy("sequence")
      .execute();
    return rows.map((row) => JSON.parse(row.event_json) as ConduitRuntimeEvent);
  }
}
