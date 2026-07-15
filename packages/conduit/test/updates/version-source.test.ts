import { test } from "bun:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { conduitVersion } from "../../src/version.js";

test("generated runtime version matches package metadata", async () => {
  const metadata: unknown = JSON.parse(
    await readFile(new URL("../../package.json", import.meta.url), "utf8"),
  );
  assert.ok(typeof metadata === "object" && metadata !== null);
  assert.ok("version" in metadata);
  assert.equal(metadata.version, conduitVersion);
});
