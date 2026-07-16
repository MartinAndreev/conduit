import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import type { HarnessRuntimeStateRepository } from "../interfaces/harness-runtime-state-repository.js";
import type { RunsDatabase } from "../interfaces/database-schema.js";
import type {
  DiagnosticArtifactRecord,
  FeaturePackageVersionRecord,
  HarnessSessionRecord,
  HarnessTurnRecord,
} from "../types/harness-runtime-state.js";

export class TursoHarnessRuntimeStateRepository implements HarnessRuntimeStateRepository {
  private readonly database;
  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RunsDatabase>(connection);
  }

  async savePackageVersion(record: FeaturePackageVersionRecord): Promise<void> {
    await this.database
      .insertInto("feature_package_versions")
      .values({
        package_version_id: record.id,
        feature_id: record.featureId,
        package_hash: record.packageHash,
        inputs_json: JSON.stringify(record.inputs),
        created_at: record.createdAt,
      })
      .onConflict((conflict) => conflict.column("package_hash").doNothing())
      .execute();
  }

  async findPackageVersion(
    packageHash: string,
  ): Promise<FeaturePackageVersionRecord | undefined> {
    const row = await this.database
      .selectFrom("feature_package_versions")
      .selectAll()
      .where("package_hash", "=", packageHash)
      .executeTakeFirst();
    return row
      ? {
          id: row.package_version_id,
          featureId: row.feature_id,
          packageHash: row.package_hash,
          inputs: JSON.parse(row.inputs_json) as string[],
          createdAt: row.created_at,
        }
      : undefined;
  }

  async saveSession(record: HarnessSessionRecord): Promise<void> {
    await this.database
      .insertInto("harness_sessions")
      .values({
        session_id: record.id,
        feature_id: record.featureId,
        package_version_id: record.packageVersionId,
        provider_id: record.providerId,
        harness: record.harness,
        harness_version: record.harnessVersion ?? null,
        protocol: record.protocol,
        model: record.model ?? null,
        native_session_id: record.nativeSessionId ?? null,
        status: record.status,
        supersedes_session_id: record.supersedesSessionId ?? null,
        created_at: record.createdAt,
        updated_at: record.updatedAt,
      })
      .onConflict((conflict) =>
        conflict.column("session_id").doUpdateSet({
          native_session_id: record.nativeSessionId ?? null,
          status: record.status,
          supersedes_session_id: record.supersedesSessionId ?? null,
          updated_at: record.updatedAt,
        }),
      )
      .execute();
  }

  async loadSession(
    sessionId: string,
  ): Promise<HarnessSessionRecord | undefined> {
    const row = await this.database
      .selectFrom("harness_sessions")
      .selectAll()
      .where("session_id", "=", sessionId)
      .executeTakeFirst();
    return row
      ? {
          id: row.session_id,
          featureId: row.feature_id,
          packageVersionId: row.package_version_id,
          providerId: row.provider_id,
          harness: row.harness,
          ...(row.harness_version
            ? { harnessVersion: row.harness_version }
            : {}),
          protocol: row.protocol,
          ...(row.model ? { model: row.model } : {}),
          ...(row.native_session_id
            ? { nativeSessionId: row.native_session_id }
            : {}),
          status: row.status as HarnessSessionRecord["status"],
          ...(row.supersedes_session_id
            ? { supersedesSessionId: row.supersedes_session_id }
            : {}),
          createdAt: row.created_at,
          updatedAt: row.updated_at,
        }
      : undefined;
  }

  async saveTurn(record: HarnessTurnRecord): Promise<void> {
    await this.database
      .insertInto("harness_turns")
      .values({
        turn_id: record.id,
        session_id: record.sessionId,
        assignment_id: record.assignmentId,
        kind: record.kind,
        status: record.status,
        started_at: record.startedAt,
        completed_at: record.completedAt ?? null,
      })
      .onConflict((conflict) =>
        conflict.column("turn_id").doUpdateSet({
          status: record.status,
          completed_at: record.completedAt ?? null,
        }),
      )
      .execute();
  }

  async saveDiagnosticArtifact(
    record: DiagnosticArtifactRecord,
  ): Promise<void> {
    await this.database
      .insertInto("diagnostic_artifacts")
      .values({
        artifact_id: record.id,
        run_id: record.runId ?? null,
        role_id: record.roleId ?? null,
        kind: record.kind,
        path: record.path,
        size_bytes: record.sizeBytes,
        truncated: record.truncated ? 1 : 0,
        created_at: record.createdAt,
        expires_at: record.expiresAt,
      })
      .onConflict((conflict) =>
        conflict.column("artifact_id").doUpdateSet({
          size_bytes: record.sizeBytes,
          truncated: record.truncated ? 1 : 0,
          expires_at: record.expiresAt,
        }),
      )
      .execute();
  }
}
