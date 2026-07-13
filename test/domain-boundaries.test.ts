import test, { describe } from "node:test";
import assert from "node:assert/strict";
import { access, readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const testDirectory = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(testDirectory, "..");
const sourceRoot = path.join(projectRoot, "src");

async function pathExists(target: string): Promise<boolean> {
  try {
    await access(target);
    return true;
  } catch {
    return false;
  }
}

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
      assert.equal(await pathExists(path.join(sourceRoot, file)), false);
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
