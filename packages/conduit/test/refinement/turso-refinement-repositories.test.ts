import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { TursoDraftRepository } from "../../src/domains/refinement/repositories/turso-draft-repository.js";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";

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
