import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  BoundedBatchWriter,
  FileProjectLockFactory,
  redactStorageDiagnostic,
  resolveGlobalDatabasePaths,
  resolveProjectDatabasePaths,
  ensureConduitStateGitIgnored,
  ensureWorktreeRootGitIgnored,
} from "../src/system/storage/index.js";

test("persistence-foundation resolves default project database path", () => {
  const paths = resolveProjectDatabasePaths("/work/project");
  assert.equal(paths.directory, "/work/project/.conduit");
  assert.equal(paths.databasePath, "/work/project/.conduit/state.db");
});

test("persistence-foundation resolves XDG global database path", () => {
  const paths = resolveGlobalDatabasePaths({ XDG_DATA_HOME: "/tmp/xdg" });
  assert.equal(paths.directory, "/tmp/xdg/conduit");
  assert.equal(paths.databasePath, "/tmp/xdg/conduit/global.db");
});

test("custom state directories ignore every runtime artifact class", async () => {
  const root = await mkdtemp(join(tmpdir(), "conduit-ignore-"));
  const stateDirectory = join(root, ".conduit-state");
  try {
    await ensureConduitStateGitIgnored(stateDirectory);
    const ignore = await readFile(join(stateDirectory, ".gitignore"), "utf8");
    for (const pattern of [
      "runs/",
      "cache/",
      "assignments/",
      "worktrees/",
      "worktree-metadata/",
      "hooks/",
      "legacy-archive/",
      "state.db.lock",
      "backups/",
      "*.db-wal",
      "*.db-shm",
      "*.lock",
    ]) {
      assert.ok(ignore.split(/\r?\n/).includes(pattern), pattern);
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a configurable worktree root ignores its complete checkout contents", async () => {
  const root = await mkdtemp(join(tmpdir(), "conduit-worktree-ignore-"));
  try {
    await ensureWorktreeRootGitIgnored(root);
    assert.equal(await readFile(join(root, ".gitignore"), "utf8"), "*\n");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("persistence-foundation project lock prevents a second owner", async () => {
  const root = await mkdtemp(join(tmpdir(), "conduit-lock-"));
  const factory = new FileProjectLockFactory();
  const first = await factory.acquire(root);
  await assert.rejects(() => factory.acquire(root), /already owned/);
  await first.release();
  const second = await factory.acquire(root);
  await second.release();
});

test("persistence-foundation project lock writes sanitized ownership metadata", async () => {
  const root = await mkdtemp(join(tmpdir(), "conduit-lock-meta-"));
  const lock = await new FileProjectLockFactory().acquire(root);
  const contents = await readFile(lock.lockPath, "utf8");
  assert.match(contents, new RegExp(String(process.pid)));
  await lock.release();
});

test("persistence-foundation replaces a lock owned by a dead process", async () => {
  const root = await mkdtemp(join(tmpdir(), "conduit-stale-lock-"));
  const stateDirectory = join(root, ".conduit");
  const lockPath = join(stateDirectory, "state.db.lock");
  await mkdir(stateDirectory, { recursive: true });
  await writeFile(lockPath, "999999999\n2026-01-01T00:00:00.000Z\n");

  try {
    const lock = await new FileProjectLockFactory().acquire(root);
    const ownership = await readFile(lock.lockPath, "utf8");
    assert.match(ownership, new RegExp(`^${process.pid}\\n`));
    await lock.release();
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("persistence-foundation redacts credential-like diagnostics", () => {
  const diagnostic = redactStorageDiagnostic(
    "token=secret-value password:super-secret api_key=abc123",
  );
  assert.equal(diagnostic.includes("secret-value"), false);
  assert.equal(diagnostic.includes("super-secret"), false);
  assert.equal(diagnostic.includes("abc123"), false);
});

test("persistence-foundation bounded batch writer rejects oversized batches", async () => {
  const seen: number[] = [];
  const writer = new BoundedBatchWriter<number>(async (item) => {
    seen.push(item);
  }, 2);
  await writer.writeBatch([1, 2]);
  assert.deepEqual(seen, [1, 2]);
  await assert.rejects(() => writer.writeBatch([1, 2, 3]), /exceeds limit/);
});
