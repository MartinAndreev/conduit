import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import type { Config } from "../../configuration/types/config.js";
import type { Run } from "../types/run.js";

type RunRole = Run["roles"][number];

/** Reads persisted run artifacts for presentation without exposing filesystem access to the TUI. */
export async function readRunRoleLog(
  projectRoot: string,
  config: Config,
  run: Run,
  role: RunRole,
): Promise<string> {
  return readFile(
    path.join(projectRoot, config.stateDir, "runs", run.id, `${role.name}.log`),
    "utf8",
  ).catch(() => "No captured output yet.");
}

/** Returns an agent worktree diff when one is available. */
export function readRunRolePatch(role: RunRole): string | undefined {
  if (!role.worktree) return undefined;
  const result = spawnSync(
    "git",
    ["-C", role.worktree, "diff", "--no-ext-diff", "--unified=3", "HEAD"],
    { encoding: "utf8" },
  );
  return result.status === 0 && result.stdout.trim()
    ? result.stdout.trim()
    : undefined;
}
