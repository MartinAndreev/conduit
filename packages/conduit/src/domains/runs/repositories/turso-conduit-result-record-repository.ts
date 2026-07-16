import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";
import type { RunsDatabase } from "../interfaces/database-schema.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type { ConduitResultRecordV1 } from "../types/agent-protocol.js";

export class TursoConduitResultRecordRepository implements ConduitResultRecordRepository {
  private readonly database;
  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RunsDatabase>(connection);
  }

  async save(input: ConduitResultRecordV1): Promise<void> {
    const record = redactPersistedValue(input);
    await this.database
      .insertInto("result_records")
      .values({
        run_id: record.runId,
        role_id: record.role,
        record_json: JSON.stringify(record),
        received_at: record.receivedAt,
      })
      .onConflict((conflict) =>
        conflict.columns(["run_id", "role_id"]).doUpdateSet({
          record_json: JSON.stringify(record),
          received_at: record.receivedAt,
        }),
      )
      .execute();
  }

  async load(
    runId: string,
    role: string,
  ): Promise<ConduitResultRecordV1 | undefined> {
    const row = await this.database
      .selectFrom("result_records")
      .select("record_json")
      .where("run_id", "=", runId)
      .where("role_id", "=", role)
      .executeTakeFirst();
    return row
      ? (JSON.parse(row.record_json) as ConduitResultRecordV1)
      : undefined;
  }
}
