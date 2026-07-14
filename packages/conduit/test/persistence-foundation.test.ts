import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  BoundedBatchWriter,
  FileProjectLockFactory,
  redactStorageDiagnostic,
  resolveGlobalDatabasePaths,
  resolveProjectDatabasePaths,
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

test("persistence-foundation redacts credential-like diagnostics", () => {
  const diagnostic = redactStorageDiagnostic("token=secret-value password:super-secret api_key=abc123");
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
