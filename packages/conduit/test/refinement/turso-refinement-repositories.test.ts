import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { TursoDraftRepository } from "../../src/domains/refinement/repositories/turso-draft-repository.js";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";
import { createSaveDraftHandler } from "../../src/domains/refinement/handlers/save-draft-handler.js";
import { createSerialTaskQueue } from "../../src/helpers/async/serial-task-queue.js";

test("Turso drafts reject stale optimistic writes", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-draft-db-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoDraftRepository(connection);
    const createdAt = "2026-01-01T00:00:00.000Z";
    await repository.save({
      featureId: "001",
      story: "First",
      testCases: "One",
      createdAt,
      updatedAt: createdAt,
    });
    const first = await repository.load("001");
    assert.equal(first?.version, 1);
    await repository.save({
      ...first!,
      story: "Second",
      updatedAt: "2026-01-02T00:00:00.000Z",
    });
    await assert.rejects(
      () =>
        repository.save({
          ...first!,
          story: "Stale",
          updatedAt: "2026-01-03T00:00:00.000Z",
        }),
      /updated by another operation/,
    );
    assert.equal((await repository.load("001"))?.story, "Second");
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("serialized UI draft writes do not conflict with an overlapping submit", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-draft-queue-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoDraftRepository(connection);
    const saveDraft = createSaveDraftHandler(repository);
    const enqueue = createSerialTaskQueue();

    const autosave = enqueue(() =>
      saveDraft({
        type: "saveDraft",
        featureId: "001",
        story: "Autosaved story",
        testCases: "Autosaved tests",
      }),
    );
    const submitSave = enqueue(() =>
      saveDraft({
        type: "saveDraft",
        featureId: "001",
        story: "Submitted story",
        testCases: "Submitted tests",
      }),
    );

    const results = await Promise.all([autosave, submitSave]);
    assert.equal(
      results.every((result) => result.success),
      true,
    );
    assert.equal((await repository.load("001"))?.story, "Submitted story");
    assert.equal((await repository.load("001"))?.version, 2);
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
