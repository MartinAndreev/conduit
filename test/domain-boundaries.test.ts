import { exists, readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { describe, expect, test } from "bun:test";

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
      expect(await exists(path.join(sourceRoot, file))).toBe(false);
    }
  });

  test("keeps filesystem, process, provider, and repository access out of the TUI", async () => {
    const files = await sourceFiles(path.join(sourceRoot, "tui"));
    for (const file of files) {
      const source = await readFile(file, "utf8");
      expect(source).not.toMatch(/from ["']node:(?:fs|child_process|crypto)/);
      expect(source).not.toMatch(
        /from ["'][^"']*domains\/[^"']+\/(?:repositories|providers)/,
      );
    }
  });
});
