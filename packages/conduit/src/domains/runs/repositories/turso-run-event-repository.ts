import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { Selectable } from "kysely";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";
import type { RunsDatabase } from "../interfaces/database-schema.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunnerEvent } from "../types/runner-events.js";

function toEvent(row: Selectable<RunsDatabase["run_events"]>): RunnerEvent {
  return {
    type: row.event_type as RunnerEvent["type"],
    runId: row.run_id,
    roleId: row.role_id,
    timestamp: row.timestamp,
    payload: JSON.parse(row.payload_json) as RunnerEvent["payload"],
  };
}

export class TursoRunEventRepository implements RunEventRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RunsDatabase>(connection);
  }

  async append(input: RunnerEvent): Promise<void> {
    const event = redactPersistedValue(input);
    await this.database.transaction().execute(async (transaction) => {
      const latest = await transaction
        .selectFrom("run_events")
        .select((expression) =>
          expression.fn.max<number>("sequence").as("sequence"),
        )
        .where("run_id", "=", event.runId)
        .executeTakeFirst();
      await transaction
        .insertInto("run_events")
        .values({
          run_id: event.runId,
          role_id: event.roleId,
          sequence: (latest?.sequence ?? 0) + 1,
          event_type: event.type,
          timestamp: event.timestamp,
          payload_json: JSON.stringify(event.payload),
        })
        .execute();
    });
  }

  async loadByRun(runId: string): Promise<readonly RunnerEvent[]> {
    return (
      await this.database
        .selectFrom("run_events")
        .selectAll()
        .where("run_id", "=", runId)
        .orderBy("sequence")
        .execute()
    ).map(toEvent);
  }

  async loadByRole(
    runId: string,
    roleId: string,
  ): Promise<readonly RunnerEvent[]> {
    return (
      await this.database
        .selectFrom("run_events")
        .selectAll()
        .where("run_id", "=", runId)
        .where("role_id", "=", roleId)
        .orderBy("sequence")
        .execute()
    ).map(toEvent);
  }

  async loadRoleIds(runId: string): Promise<readonly string[]> {
    const rows = await this.database
      .selectFrom("run_events")
      .select("role_id")
      .select((expression) =>
        expression.fn.min<number>("sequence").as("first_sequence"),
      )
      .where("run_id", "=", runId)
      .groupBy("role_id")
      .orderBy("first_sequence")
      .execute();
    return rows.map((row) => row.role_id);
  }

  async clear(runId: string): Promise<void> {
    await this.database
      .deleteFrom("run_events")
      .where("run_id", "=", runId)
      .execute();
  }
}
