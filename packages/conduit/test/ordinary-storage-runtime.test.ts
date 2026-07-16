import { test } from "bun:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

test("embedded Turso opens and prepares statements under ordinary Node", async () => {
  const directory = await mkdtemp(join(tmpdir(), "conduit-node-storage-"));
  try {
    const resultPath = join(directory, "result.json");
    execFileSync(
      "node",
      [
        "--import",
        "tsx",
        "test/fixtures/node-storage-smoke.ts",
        join(directory, "project"),
        join(directory, "global"),
        resultPath,
      ],
      { stdio: "pipe" },
    );
    const result = JSON.parse(await readFile(resultPath, "utf8")) as {
      projectMigrations: number;
      globalMigrations: number;
    };
    assert.equal(result.projectMigrations, 5);
    assert.equal(result.globalMigrations, 1);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
