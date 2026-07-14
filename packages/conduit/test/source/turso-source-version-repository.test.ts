import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";
import { TursoSourceVersionRepository } from "../../src/domains/source/repositories/turso-source-version-repository.js";

test("source-version primitives persist ordered evidence metadata for Feature 003", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-source-version-"));
  try {
    const connection = await new ProjectDatabaseFactory(projectRoot).open();
    const repository = new TursoSourceVersionRepository(connection);
    await repository.save({
      sourcePath: "src/index.ts",
      sourceVersion: "v1",
      contentChecksum: "checksum-1",
      observedAt: "2026-01-01T00:00:00.000Z",
      metadata: { commit: "abc" },
    });
    await repository.save({
      sourcePath: "src/index.ts",
      sourceVersion: "v2",
      contentChecksum: "checksum-2",
      observedAt: "2026-01-02T00:00:00.000Z",
      metadata: { commit: "def" },
    });

    assert.equal(
      (await repository.load("src/index.ts", "v1"))?.metadata.commit,
      "abc",
    );
    assert.deepEqual(
      (await repository.listBySource("src/index.ts")).map(
        ({ sourceVersion }) => sourceVersion,
      ),
      ["v2", "v1"],
    );
    await connection.close();
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
