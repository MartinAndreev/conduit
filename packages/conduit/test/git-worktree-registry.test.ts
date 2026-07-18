import { test } from "bun:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { GitWorktreeRegistryService } from "../src/system/git/services/git-worktree-registry-service.js";

async function fixture() {
  const root = await mkdtemp(path.join(tmpdir(), "conduit-git-worktrees-"));
  execFileSync("git", ["-C", root, "init"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Test"]);
  await writeFile(path.join(root, "README.md"), "base\n");
  execFileSync("git", ["-C", root, "add", "."]);
  execFileSync("git", [
    "-C",
    root,
    "-c",
    "commit.gpgSign=false",
    "commit",
    "-m",
    "base",
  ]);
  return root;
}

test("Git worktree registry parses and removes only an exact prunable registration", async () => {
  const root = await fixture();
  const workspace = path.join(root, ".slots", "worker");
  const branch = "conduit/legacy/worker";
  try {
    execFileSync("git", [
      "-C",
      root,
      "worktree",
      "add",
      "-b",
      branch,
      workspace,
    ]);
    const head = execFileSync("git", ["-C", root, "rev-parse", "HEAD"], {
      encoding: "utf8",
    }).trim();
    await rm(workspace, { recursive: true, force: true });
    const service = new GitWorktreeRegistryService();
    assert.deepEqual(service.find(root, workspace), {
      workspacePath: path.resolve(workspace),
      head,
      branch,
      prunable: true,
      locked: false,
    });
    assert.equal(
      service.remove(root, path.join(root, ".slots", "other")),
      false,
    );
    assert.equal(service.remove(root, workspace), true);
    assert.equal(service.find(root, workspace), undefined);
    assert.equal(
      execFileSync("git", ["-C", root, "rev-parse", `refs/heads/${branch}`], {
        encoding: "utf8",
      }).trim(),
      head,
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("Git worktree registry refuses to remove a locked registration", async () => {
  const root = await fixture();
  const workspace = path.join(root, ".slots", "reviewer");
  try {
    execFileSync("git", [
      "-C",
      root,
      "worktree",
      "add",
      "-b",
      "conduit/locked/reviewer",
      workspace,
    ]);
    execFileSync("git", ["-C", root, "worktree", "lock", workspace]);
    const service = new GitWorktreeRegistryService();
    assert.equal(service.find(root, workspace)?.locked, true);
    assert.equal(service.remove(root, workspace), false);
    assert.ok(service.find(root, workspace));
  } finally {
    execFileSync("git", ["-C", root, "worktree", "unlock", workspace]);
    await rm(root, { recursive: true, force: true });
  }
});
