import { createHash } from "node:crypto";
import { realpathSync } from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import type { RepositoryIdentity } from "../types/role-workspace.js";

const roleKeyPattern = /^[a-z0-9](?:[a-z0-9._-]{0,78}[a-z0-9])?$/;

export function resolveRepositoryIdentity(
  projectRoot: string,
): RepositoryIdentity {
  const root = realpathSync.native(path.resolve(projectRoot));
  const result = spawnSync(
    "git",
    ["-C", root, "rev-parse", "--git-common-dir"],
    { encoding: "utf8" },
  );
  if (result.status !== 0 || !result.stdout.trim())
    throw new Error("Role workspaces require a Git common directory.");
  const commonDirectory = realpathSync.native(
    path.resolve(root, result.stdout.trim()),
  );
  return {
    repositoryId: createHash("sha256")
      .update(commonDirectory, "utf8")
      .digest("hex"),
    commonDirectory,
  };
}

export function normalizeRoleWorkspaceKey(roleId: string): string {
  const normalized = roleId.normalize("NFC").trim().toLowerCase();
  if (
    !roleKeyPattern.test(normalized) ||
    normalized === "." ||
    normalized === ".."
  )
    throw new Error(
      `Role ID ${JSON.stringify(roleId)} cannot be used as a workspace key.`,
    );
  return normalized;
}

export function assertDistinctRoleWorkspaceKeys(
  roleIds: readonly string[],
): void {
  const owners = new Map<string, string>();
  for (const roleId of roleIds) {
    const key = normalizeRoleWorkspaceKey(roleId);
    const existing = owners.get(key);
    if (existing && existing !== roleId)
      throw new Error(
        `Role IDs ${JSON.stringify(existing)} and ${JSON.stringify(roleId)} collide as workspace key ${key}.`,
      );
    owners.set(key, roleId);
  }
}

export function roleWorkspaceSlotPath(
  worktreeRoot: string,
  repositoryId: string,
  roleKey: string,
): string {
  if (!/^[a-f0-9]{64}$/.test(repositoryId))
    throw new Error("Repository workspace identity is invalid.");
  const normalizedRoleKey = normalizeRoleWorkspaceKey(roleKey);
  const root = path.resolve(worktreeRoot);
  const slot = path.resolve(root, repositoryId, normalizedRoleKey);
  const relative = path.relative(root, slot);
  if (
    relative === ".." ||
    relative.startsWith(`..${path.sep}`) ||
    path.isAbsolute(relative)
  )
    throw new Error("Role workspace slot escaped the configured root.");
  return slot;
}
