import { test } from "bun:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, mkdir, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  assertDistinctRoleWorkspaceKeys,
  normalizeRoleWorkspaceKey,
  resolveRepositoryIdentity,
  roleWorkspaceSlotPath,
} from "../../src/domains/runs/services/role-workspace-identity-service.js";

function init(root: string): void {
  execFileSync("git", ["-C", root, "init"]);
}

test("repository identity uses the real Git common directory", async () => {
  const parentA = await mkdtemp(path.join(tmpdir(), "conduit-repo-a-"));
  const parentB = await mkdtemp(path.join(tmpdir(), "conduit-repo-b-"));
  const repoA = path.join(parentA, "same-name");
  const repoB = path.join(parentB, "same-name");
  const link = path.join(parentA, "repository-link");
  const linkedWorktree = path.join(parentA, "linked-worktree");
  try {
    await mkdir(repoA);
    await mkdir(repoB);
    init(repoA);
    init(repoB);
    execFileSync("git", [
      "-C",
      repoA,
      "config",
      "user.email",
      "test@example.com",
    ]);
    execFileSync("git", ["-C", repoA, "config", "user.name", "Test"]);
    execFileSync("git", [
      "-C",
      repoA,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "--allow-empty",
      "-m",
      "base",
    ]);
    execFileSync("git", [
      "-C",
      repoA,
      "worktree",
      "add",
      "-b",
      "identity-test",
      linkedWorktree,
    ]);
    await symlink(repoA, link, "dir");
    const first = resolveRepositoryIdentity(repoA);
    const linked = resolveRepositoryIdentity(link);
    const worktreeIdentity = resolveRepositoryIdentity(linkedWorktree);
    const unrelated = resolveRepositoryIdentity(repoB);
    assert.equal(linked.repositoryId, first.repositoryId);
    assert.equal(linked.commonDirectory, first.commonDirectory);
    assert.equal(worktreeIdentity.repositoryId, first.repositoryId);
    assert.equal(worktreeIdentity.commonDirectory, first.commonDirectory);
    assert.notEqual(unrelated.repositoryId, first.repositoryId);
  } finally {
    await rm(parentA, { recursive: true, force: true });
    await rm(parentB, { recursive: true, force: true });
  }
});

test("role workspace keys reject unsafe and case-colliding identities", () => {
  assert.equal(normalizeRoleWorkspaceKey("Reviewer"), "reviewer");
  for (const unsafe of [
    "../reviewer",
    "a/b",
    "a\\b",
    `reviewer ${String.fromCharCode(0)}`,
    "réviewer",
  ])
    assert.throws(() => normalizeRoleWorkspaceKey(unsafe));
  assert.throws(() => assertDistinctRoleWorkspaceKeys(["QA", "qa"]), /collide/);
  assert.doesNotThrow(() =>
    assertDistinctRoleWorkspaceKeys(["frontend", "quality-assurance"]),
  );
});

test("role workspace slot paths remain under repository and role identity", () => {
  const repositoryId = "a".repeat(64);
  const root = path.join(tmpdir(), "conduit-workspace-root");
  assert.equal(
    roleWorkspaceSlotPath(root, repositoryId, "Reviewer"),
    path.join(root, repositoryId, "reviewer"),
  );
  assert.throws(() => roleWorkspaceSlotPath(root, "invalid", "reviewer"));
  assert.throws(() => roleWorkspaceSlotPath(root, repositoryId, "../reviewer"));
});
