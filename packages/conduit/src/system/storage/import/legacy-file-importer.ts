import { createHash } from "node:crypto";
import { mkdir, readFile, readdir, rename } from "node:fs/promises";
import { basename, dirname, join, relative } from "node:path";
import { createTursoKysely } from "../adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "../interfaces/database.js";
import type { LegacyImportDatabase } from "../interfaces/import-database.js";
import type {
  LegacyImportResult,
  LegacyImportRunner,
} from "../interfaces/startup-migration.js";
import {
  redactPersistedValue,
  redactSecrets,
} from "../security/secret-redaction.js";
import { TursoDraftRepository } from "../../../domains/refinement/repositories/turso-draft-repository.js";
import { TursoRunEventRepository } from "../../../domains/runs/repositories/turso-run-event-repository.js";
import { TursoReviewResultRepository } from "../../../domains/runs/repositories/turso-review-result-repository.js";
import { TursoRunRecoveryRepository } from "../../../domains/runs/repositories/turso-run-recovery-repository.js";
import type { RefinementDraft } from "../../../domains/refinement/types/draft.js";
import type { RunnerEvent } from "../../../domains/runs/types/runner-events.js";
import { RunnerEventProvenance } from "../../../domains/runs/enums/runner-event-provenance.js";
import type { ReviewResult } from "../../../domains/runs/types/review.js";
import type { Run } from "../../../domains/runs/types/run.js";
import type { RefinementRevision } from "../../../domains/refinement/types/revision.js";
import { TursoResearchReportRepository } from "../../../domains/refinement/repositories/turso-research-report-repository.js";

async function filesIn(directory: string): Promise<readonly string[]> {
  const entries = await readdir(directory, { withFileTypes: true }).catch(
    () => [],
  );
  return entries
    .filter((entry) => entry.isFile())
    .map((entry) => join(directory, entry.name));
}

async function directoriesIn(directory: string): Promise<readonly string[]> {
  const entries = await readdir(directory, { withFileTypes: true }).catch(
    () => [],
  );
  return entries
    .filter((entry) => entry.isDirectory())
    .map((entry) => join(directory, entry.name));
}

function checksum(content: string): string {
  return createHash("sha256").update(content).digest("hex");
}

export class LegacyFileImporter implements LegacyImportRunner {
  constructor(
    private readonly projectRoot: string,
    private readonly stateDirectory: string,
    private readonly specsDirectory: string,
  ) {}

  async import(connection: DatabaseConnection): Promise<LegacyImportResult> {
    const database = createTursoKysely<LegacyImportDatabase>(connection);
    const drafts = new TursoDraftRepository(connection);
    const events = new TursoRunEventRepository(connection);
    const reviews = new TursoReviewResultRepository(connection);
    const recovery = new TursoRunRecoveryRepository(connection);
    const research = new TursoResearchReportRepository(connection);
    let importedRecords = 0;
    let skippedImports = 0;

    const archive = async (sourcePath: string): Promise<void> => {
      const stateRelativePath = relative(this.stateDirectory, sourcePath);
      if (stateRelativePath.startsWith("..")) return;
      const destination = join(
        this.stateDirectory,
        "legacy-archive",
        stateRelativePath,
      );
      await mkdir(dirname(destination), { recursive: true });
      await rename(sourcePath, destination);
    };

    const processFile = async (
      sourcePath: string,
      consume: (content: string) => Promise<number>,
    ) => {
      const content = await readFile(sourcePath, "utf8");
      const sourceChecksum = checksum(content);
      const existing = await database
        .selectFrom("import_ledger")
        .select(["source_checksum", "status"])
        .where("source_path", "=", sourcePath)
        .executeTakeFirst();
      if (existing?.status === "succeeded") {
        skippedImports += 1;
        return;
      }
      try {
        const count = await consume(content);
        importedRecords += count;
        await database
          .insertInto("import_ledger")
          .values({
            source_path: sourcePath,
            source_checksum: sourceChecksum,
            imported_at: new Date().toISOString(),
            record_count: count,
            status: "succeeded",
            diagnostic: null,
          })
          .onConflict((conflict) =>
            conflict.column("source_path").doUpdateSet({
              source_checksum: sourceChecksum,
              imported_at: new Date().toISOString(),
              record_count: count,
              status: "succeeded",
              diagnostic: null,
            }),
          )
          .execute();
        await archive(sourcePath);
      } catch (error) {
        skippedImports += 1;
        await database
          .insertInto("import_ledger")
          .values({
            source_path: sourcePath,
            source_checksum: sourceChecksum,
            imported_at: new Date().toISOString(),
            record_count: 0,
            status: "failed",
            diagnostic: redactSecrets(
              error instanceof Error ? error.message : String(error),
            ),
          })
          .onConflict((conflict) =>
            conflict.column("source_path").doUpdateSet({
              source_checksum: sourceChecksum,
              imported_at: new Date().toISOString(),
              record_count: 0,
              status: "failed",
              diagnostic: redactSecrets(
                error instanceof Error ? error.message : String(error),
              ),
            }),
          )
          .execute();
      }
    };

    for (const sourcePath of await filesIn(
      join(this.stateDirectory, "drafts"),
    )) {
      if (!sourcePath.endsWith(".json")) continue;
      await processFile(sourcePath, async (content) => {
        const parsed = JSON.parse(content) as RefinementDraft;
        const existing = await drafts.load(parsed.featureId);
        await drafts.save({ ...parsed, version: existing?.version });
        return 1;
      });
    }

    for (const runDirectory of await directoriesIn(
      join(this.stateDirectory, "runs"),
    )) {
      const runId = basename(runDirectory);
      const eventsPath = join(runDirectory, "events.json");
      if (
        (await readFile(eventsPath, "utf8").catch(() => undefined)) !==
        undefined
      ) {
        await processFile(eventsPath, async (content) => {
          const parsed = JSON.parse(content) as {
            events?: readonly RunnerEvent[];
          };
          await events.clear(runId);
          for (const event of parsed.events ?? []) await events.append(event);
          return parsed.events?.length ?? 0;
        });
      }
      const reviewPath = join(runDirectory, "review.json");
      if (
        (await readFile(reviewPath, "utf8").catch(() => undefined)) !==
        undefined
      ) {
        await processFile(reviewPath, async (content) => {
          await reviews.save(JSON.parse(content) as ReviewResult);
          return 1;
        });
      }
      const snapshotPath = join(runDirectory, "run.json");
      if (
        (await readFile(snapshotPath, "utf8").catch(() => undefined)) !==
        undefined
      ) {
        await processFile(snapshotPath, async (content) => {
          await recovery.saveSnapshot(JSON.parse(content) as Run);
          return 1;
        });
      }
      for (const artifactPath of await filesIn(runDirectory)) {
        const artifactName = basename(artifactPath);
        const kind = artifactName.endsWith(".log")
          ? "transcript"
          : artifactName.endsWith(".patch") || artifactName.endsWith(".diff")
            ? "diff"
            : undefined;
        if (!kind) continue;
        await processFile(artifactPath, async (content) => {
          const sanitized = redactSecrets(content);
          await events.append({
            type: "tool-output",
            provenance: RunnerEventProvenance.ConduitObserved,
            runId,
            roleId: artifactName.replace(/\.(?:log|patch|diff)$/, ""),
            timestamp: new Date().toISOString(),
            payload: {
              kind: "tool-output",
              tool: `legacy ${kind} artifact`,
              output: sanitized.slice(0, 4_000),
              truncated: sanitized.length > 4_000,
            },
          });
          return 1;
        });
      }
    }

    for (const featureDirectory of await directoriesIn(this.specsDirectory)) {
      const featureId = basename(featureDirectory).match(/^(\d{3})-/)?.[1];
      if (!featureId) continue;
      const researchPath = join(featureDirectory, "research.md");
      if (
        (await readFile(researchPath, "utf8").catch(() => undefined)) !==
        undefined
      ) {
        await processFile(researchPath, async (content) => {
          await research.save(featureId, content);
          return 1;
        });
      }
      for (const revisionDirectory of await directoriesIn(
        join(featureDirectory, "revisions"),
      )) {
        const metadataPath = join(revisionDirectory, "revision.json");
        if (
          (await readFile(metadataPath, "utf8").catch(() => undefined)) ===
          undefined
        )
          continue;
        await processFile(metadataPath, async (content) => {
          const revision = redactPersistedValue(
            JSON.parse(content) as RefinementRevision,
          );
          const questions = await readFile(
            join(revisionDirectory, "questions.md"),
            "utf8",
          ).catch(() => null);
          const answers = await readFile(
            join(revisionDirectory, "answers.md"),
            "utf8",
          ).catch(() => null);
          const transcript = await readFile(
            join(revisionDirectory, "architect-run.md"),
            "utf8",
          ).catch(() => null);
          await database
            .insertInto("refinement_revisions")
            .values({
              feature_id: featureId,
              revision_id: revision.id,
              status: revision.status,
              directory: featureDirectory,
              feedback: revision.feedback ?? null,
              questions_source: questions ? redactSecrets(questions) : null,
              answers: answers ? redactSecrets(answers) : null,
              review_decision: null,
              review_feedback: null,
              transcript: transcript ? redactSecrets(transcript) : null,
              created_at: revision.createdAt,
              updated_at: revision.updatedAt,
              version: revision.version ?? 1,
            })
            .onConflict((conflict) =>
              conflict.columns(["feature_id", "revision_id"]).doNothing(),
            )
            .execute();
          return 1;
        });
      }
    }

    await database.destroy();
    return { importedRecords, skippedImports };
  }
}
