import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import {
  redactPersistedValue,
  redactSecrets,
} from "@system/storage/security/secret-redaction.js";
import type { RunsDatabase } from "../interfaces/database-schema.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import type { RunRecoveryState, RunSnapshot } from "../types/recovery.js";
import type { Run } from "../types/run.js";

function recoveryState(run: Run): RunRecoveryState {
  if (run.status === "planned") return "planned";
  if (run.status === "completed") return "complete";
  if (run.status === "cancelled") return "cancelled";
  if (run.status === "failed") return "interrupted";
  return "running";
}

export class TursoRunRecoveryRepository implements RunRecoveryRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RunsDatabase>(connection);
  }

  async saveSnapshot(run: Run, expectedVersion?: number): Promise<RunSnapshot> {
    const existing = await this.loadSnapshot(run.id);
    if (
      expectedVersion !== undefined &&
      existing?.version !== expectedVersion
    ) {
      throw new Error(
        `Run ${run.id} snapshot was updated by another operation.`,
      );
    }
    const sanitized = redactPersistedValue(run);
    const now = new Date().toISOString();
    const version = (existing?.version ?? 0) + 1;
    const values: RunsDatabase["run_snapshots"] = {
      run_id: run.id,
      snapshot_json: JSON.stringify(sanitized),
      status: run.status,
      version,
      updated_at: now,
    };
    await this.database
      .insertInto("run_snapshots")
      .values(values)
      .onConflict((conflict) => conflict.column("run_id").doUpdateSet(values))
      .execute();
    return {
      run: sanitized,
      state: recoveryState(run),
      version,
      updatedAt: now,
    };
  }

  async loadSnapshot(runId: string): Promise<RunSnapshot | undefined> {
    const row = await this.database
      .selectFrom("run_snapshots")
      .selectAll()
      .where("run_id", "=", runId)
      .executeTakeFirst();
    if (!row) return undefined;
    const run = JSON.parse(row.snapshot_json) as Run;
    return {
      run,
      state: recoveryState(run),
      version: row.version,
      updatedAt: row.updated_at,
    };
  }

  async listSnapshots(limit = 20): Promise<readonly RunSnapshot[]> {
    const rows = await this.database
      .selectFrom("run_snapshots")
      .selectAll()
      .orderBy("updated_at", "desc")
      .limit(limit)
      .execute();
    return rows.map((row) => {
      const run = JSON.parse(row.snapshot_json) as Run;
      return {
        run,
        state: recoveryState(run),
        version: row.version,
        updatedAt: row.updated_at,
      };
    });
  }

  async markInterrupted(runId: string, diagnostic?: string): Promise<void> {
    await this.markRecovery(runId, "interrupted", diagnostic);
  }

  async markCancelled(runId: string): Promise<void> {
    await this.markRecovery(runId, "cancelled");
  }

  private async markRecovery(
    runId: string,
    state: "interrupted" | "cancelled",
    diagnostic?: string,
  ): Promise<void> {
    const values: RunsDatabase["run_recovery"] = {
      run_id: runId,
      state,
      diagnostic: diagnostic ? redactSecrets(diagnostic) : null,
      updated_at: new Date().toISOString(),
    };
    await this.database
      .insertInto("run_recovery")
      .values(values)
      .onConflict((conflict) => conflict.column("run_id").doUpdateSet(values))
      .execute();
  }
}
