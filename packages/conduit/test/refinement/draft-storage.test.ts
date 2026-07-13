import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { FileDraftRepository } from "../../src/domains/refinement/repositories/file-draft-repository.js";
import type { RefinementDraft } from "../../src/domains/refinement/types/draft.js";

test("FileDraftRepository saves and loads drafts", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "conduit-draft-test-"));
  try {
    const repository = new FileDraftRepository(tempDir);
    const draft: RefinementDraft = {
      featureId: "001",
      story: "Test story",
      testCases: "Test cases",
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    const draftPath = await repository.save(draft);
    assert.ok(draftPath.endsWith("001.json"));

    const loaded = await repository.load("001");
    assert.ok(loaded);
    assert.equal(loaded.featureId, "001");
    assert.equal(loaded.story, "Test story");
    assert.equal(loaded.testCases, "Test cases");
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("FileDraftRepository returns null for non-existent drafts", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "conduit-draft-test-"));
  try {
    const repository = new FileDraftRepository(tempDir);
    const loaded = await repository.load("non-existent");
    assert.equal(loaded, null);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("FileDraftRepository discards drafts", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "conduit-draft-test-"));
  try {
    const repository = new FileDraftRepository(tempDir);
    const draft: RefinementDraft = {
      featureId: "001",
      story: "Test story",
      testCases: "Test cases",
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    await repository.save(draft);
    const discarded = await repository.discard("001");
    assert.equal(discarded, true);

    const loaded = await repository.load("001");
    assert.equal(loaded, null);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("FileDraftRepository lists all drafts", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "conduit-draft-test-"));
  try {
    const repository = new FileDraftRepository(tempDir);

    const draft1: RefinementDraft = {
      featureId: "001",
      story: "Story 1",
      testCases: "Cases 1",
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    const draft2: RefinementDraft = {
      featureId: "002",
      story: "Story 2",
      testCases: "Cases 2",
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    await repository.save(draft1);
    await repository.save(draft2);

    const drafts = await repository.list();
    assert.equal(drafts.length, 2);
    assert.ok(drafts.some((d) => d.featureId === "001"));
    assert.ok(drafts.some((d) => d.featureId === "002"));
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
