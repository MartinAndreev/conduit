import { appendFile, mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";

const REQUIRED_PATTERNS = [
  "runs/",
  "cache/",
  "assignments/",
  "worktrees/",
  "worktree-metadata/",
  "hooks/",
  "legacy-archive/",
  "state.db",
  "state.db-wal",
  "state.db-shm",
  "state.db.lock",
  "backups/",
  "*.db",
  "*.db-wal",
  "*.db-shm",
  "*.lock",
] as const;

export async function ensureConduitStateGitIgnored(
  stateDirectory: string,
): Promise<void> {
  await ensureInternalGitIgnore(stateDirectory, REQUIRED_PATTERNS);
}

export async function ensureWorktreeRootGitIgnored(
  worktreeRoot: string,
): Promise<void> {
  await ensureInternalGitIgnore(worktreeRoot, ["*"]);
}

async function ensureInternalGitIgnore(
  directory: string,
  requiredPatterns: readonly string[],
): Promise<void> {
  const ignorePath = join(directory, ".gitignore");
  await mkdir(directory, { recursive: true });
  let existing = "";
  try {
    existing = await readFile(ignorePath, "utf8");
  } catch {
    existing = "";
  }
  const missing = requiredPatterns.filter(
    (pattern) => !existing.split(/\r?\n/).includes(pattern),
  );
  if (missing.length > 0) {
    const prefix = existing.length > 0 && !existing.endsWith("\n") ? "\n" : "";
    await appendFile(ignorePath, `${prefix}${missing.join("\n")}\n`, "utf8");
  }
}
