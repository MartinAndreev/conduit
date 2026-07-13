import assert from "node:assert/strict";
import { describe, test } from "bun:test";
import { exists, readdir, readFile } from "node:fs/promises";
import path from "node:path";

const projectRoot = path.resolve(import.meta.dir, "..");
const sourceRoot = path.join(projectRoot, "src");

async function sourceFiles(directory: string): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map(async (entry) => {
      const entryPath = path.join(directory, entry.name);
      return entry.isDirectory()
        ? sourceFiles(entryPath)
        : entry.name.endsWith(".ts") || entry.name.endsWith(".tsx")
          ? [entryPath]
          : [];
    }),
  );
  return nested.flat();
}

describe("domain boundaries", () => {
  test("does not retain legacy root application modules", async () => {
    for (const file of [
      "config.ts",
      "features.ts",
      "runs.ts",
      "skills.ts",
      "role-templates.ts",
      "commands",
    ]) {
      assert.equal(await exists(path.join(sourceRoot, file)), false);
    }
  });

  test("keeps filesystem, process, provider, and repository access out of the TUI", async () => {
    const files = await sourceFiles(path.join(sourceRoot, "tui"));
    for (const file of files) {
      const source = await readFile(file, "utf8");
      assert.doesNotMatch(source, /from ["']node:(?:fs|child_process|crypto)/);
      assert.doesNotMatch(
        source,
        /from ["'][^"']*domains\/[^"']+\/(?:repositories|providers)/,
      );
    }
  });
});
