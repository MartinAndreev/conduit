import { test } from "bun:test";
import assert from "node:assert/strict";
import { tmpdir } from "node:os";
import path from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { InMemoryCredentialStore } from "../../src/domains/credentials/repositories/in-memory-store.js";
import {
  EncryptedFallbackStore,
  CompositeCredentialStore,
} from "../../src/domains/credentials/repositories/encrypted-fallback-store.js";

test("CompositeCredentialStore initializes fallback before probing primary", async () => {
  const tempDir = await mkdtemp(path.join(tmpdir(), "cred-test-"));
  try {
    const fallback = new EncryptedFallbackStore(tempDir);
    const primary = new InMemoryCredentialStore();
    const composite = new CompositeCredentialStore(primary, fallback);

    await composite.initialize();

    const result = await composite.get("test", "key");
    assert.equal(result, undefined);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("CompositeCredentialStore uses fallback when primary initialization fails", async () => {
  const tempDir = await mkdtemp(path.join(tmpdir(), "cred-test-"));
  try {
    const fallback = new EncryptedFallbackStore(tempDir);
    const failingPrimary = {
      async initialize() {
        throw new Error("vault unavailable");
      },
      async get() {
        throw new Error("not available");
      },
      async set() {
        throw new Error("not available");
      },
      async delete() {
        throw new Error("not available");
      },
      async list() {
        throw new Error("not available");
      },
    };
    const composite = new CompositeCredentialStore(failingPrimary, fallback);

    await composite.initialize();
    assert.equal(composite.isUsingFallback(), true);

    await composite.set("profile1", "apiKey", "secret123");
    const value = await composite.get("profile1", "apiKey");
    assert.equal(value, "secret123");
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("EncryptedFallbackStore set and get round-trips credentials", async () => {
  const tempDir = await mkdtemp(path.join(tmpdir(), "cred-test-"));
  try {
    const store = new EncryptedFallbackStore(tempDir);
    await store.initialize();

    await store.set("myProfile", "token", "abc123");
    const value = await store.get("myProfile", "token");
    assert.equal(value, "abc123");

    const keys = await store.list("myProfile");
    assert.deepEqual(keys, ["token"]);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("EncryptedFallbackStore persists across instances", async () => {
  const tempDir = await mkdtemp(path.join(tmpdir(), "cred-test-"));
  try {
    const store1 = new EncryptedFallbackStore(tempDir);
    await store1.initialize();
    await store1.set("p", "k", "v1");

    const store2 = new EncryptedFallbackStore(tempDir);
    await store2.initialize();
    const value = await store2.get("p", "k");
    assert.equal(value, "v1");
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("EncryptedFallbackStore throws before initialize", async () => {
  const tempDir = await mkdtemp(path.join(tmpdir(), "cred-test-"));
  try {
    const store = new EncryptedFallbackStore(tempDir);
    await assert.rejects(() => store.get("p", "k"), {
      code: "VAULT_NOT_INITIALIZED",
    });
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
