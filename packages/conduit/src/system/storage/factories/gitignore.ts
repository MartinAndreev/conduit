import { appendFile, mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";

const REQUIRED_PATTERNS = [
  "state.db",
  "state.db-wal",
  "state.db-shm",
  "backups/",
  "*.db",
] as const;

export async function ensureConduitStateGitIgnored(
  stateDirectory: string,
): Promise<void> {
  const ignorePath = join(stateDirectory, ".gitignore");
  await mkdir(stateDirectory, { recursive: true });
  let existing = "";
  try {
    existing = await readFile(ignorePath, "utf8");
  } catch {
    existing = "";
  }
  const missing = REQUIRED_PATTERNS.filter(
    (pattern) => !existing.split(/\r?\n/).includes(pattern),
  );
  if (missing.length > 0) {
    const prefix = existing.length > 0 && !existing.endsWith("\n") ? "\n" : "";
    await appendFile(ignorePath, `${prefix}${missing.join("\n")}\n`, "utf8");
  }
}
