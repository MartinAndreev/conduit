import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { TursoDraftRepository } from "../src/domains/refinement/repositories/turso-draft-repository.js";
import { TursoRunEventRepository } from "../src/domains/runs/repositories/turso-run-event-repository.js";
import { ProjectDatabaseFactory } from "../src/system/storage/factories/database-factories.js";
import { LegacyFileImporter } from "../src/system/storage/import/legacy-file-importer.js";
import { DefaultStartupMigrationRunner } from "../src/system/storage/migrations/startup-migration-runner.js";

test("startup blocks through schema and idempotent legacy import before use", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-import-"));
  const globalRoot = await mkdtemp(join(tmpdir(), "conduit-global-"));
  const stateDirectory = join(projectRoot, ".conduit");
  const specsDirectory = join(projectRoot, "specs");
  const draftPath = join(stateDirectory, "drafts", "001.json");
  await mkdir(join(stateDirectory, "drafts"), { recursive: true });
  const runDirectory = join(stateDirectory, "runs", "legacy-run");
  await mkdir(runDirectory, { recursive: true });
  await mkdir(specsDirectory, { recursive: true });
  await writeFile(
    draftPath,
    JSON.stringify({
      featureId: "001",
      story: "Keep token=legacy-secret out of storage",
      testCases: "Migrates once",
      createdAt: "2026-01-01T00:00:00.000Z",
      updatedAt: "2026-01-01T00:00:00.000Z",
      version: 1,
    }),
  );
  await writeFile(
    join(runDirectory, "backend.log"),
    "runner output token=legacy-transcript-secret",
  );
  await writeFile(
    join(runDirectory, "backend.diff"),
    "diff metadata password=legacy-diff-secret",
  );
  const importer = new LegacyFileImporter(
    projectRoot,
    stateDirectory,
    specsDirectory,
  );
  const runner = new DefaultStartupMigrationRunner(
    projectRoot,
    ".conduit",
    importer,
    { XDG_DATA_HOME: globalRoot },
  );
  try {
    const stages: string[] = [];
    const first = await runner.run((progress) => stages.push(progress.stage));
    assert.deepEqual(stages, [
      "global-schema",
      "project-schema",
      "legacy-import",
      "complete",
    ]);
    assert.equal(first.importedRecords, 3);

    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const draft = await new TursoDraftRepository(connection).load("001");
    assert.ok(draft);
    assert.equal(draft.story.includes("legacy-secret"), false);
    const importedArtifacts = await new TursoRunEventRepository(
      connection,
    ).loadByRun("legacy-run");
    assert.equal(importedArtifacts.length, 2);
    assert.doesNotMatch(JSON.stringify(importedArtifacts), /legacy-.*-secret/);
    await connection.close();

    const second = await runner.run();
    assert.equal(second.importedRecords, 0);
    assert.equal(second.skippedImports, 3);
    assert.ok(await readFile(draftPath, "utf8"));
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(globalRoot, { recursive: true, force: true });
  }
});

test("invalid legacy JSON is recorded and left in place", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-import-bad-"));
  const globalRoot = await mkdtemp(join(tmpdir(), "conduit-global-bad-"));
  const stateDirectory = join(projectRoot, ".conduit");
  const draftPath = join(stateDirectory, "drafts", "bad.json");
  await mkdir(join(stateDirectory, "drafts"), { recursive: true });
  await writeFile(draftPath, "{token=diagnostic-secret");
  const runner = new DefaultStartupMigrationRunner(
    projectRoot,
    ".conduit",
    new LegacyFileImporter(
      projectRoot,
      stateDirectory,
      join(projectRoot, "specs"),
    ),
    { XDG_DATA_HOME: globalRoot },
  );
  try {
    const result = await runner.run();
    assert.equal(result.skippedImports, 1);
    assert.equal(await readFile(draftPath, "utf8"), "{token=diagnostic-secret");
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
    await rm(globalRoot, { recursive: true, force: true });
  }
});
