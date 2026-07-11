import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { FileDraftRepository } from "../../src/domains/refinement/repositories/file-draft-repository.js";
import { createSaveDraftHandler } from "../../src/domains/refinement/handlers/save-draft-handler.js";
import { createDiscardDraftHandler } from "../../src/domains/refinement/handlers/discard-draft-handler.js";
import { createResumeDraftHandler } from "../../src/domains/refinement/handlers/resume-draft-handler.js";
import { createGetDraftHandler } from "../../src/domains/refinement/handlers/get-draft-handler.js";
import { createListDraftsHandler } from "../../src/domains/refinement/handlers/list-drafts-handler.js";

test("saveDraft handler saves draft correctly", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-handler-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const handler = createSaveDraftHandler(repository);

    const result = await handler({
      type: "saveDraft",
      featureId: "001",
      story: "Test story",
      testCases: "Test cases",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.equal(result.data.saved, true);
      assert.ok(result.data.draftPath.endsWith("001.json"));
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("discardDraft handler discards draft correctly", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-handler-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const saveHandler = createSaveDraftHandler(repository);
    const discardHandler = createDiscardDraftHandler(repository);

    await saveHandler({
      type: "saveDraft",
      featureId: "001",
      story: "Test story",
      testCases: "Test cases",
    });

    const result = await discardHandler({
      type: "discardDraft",
      featureId: "001",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.equal(result.data.discarded, true);
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("resumeDraft handler resumes draft correctly", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-handler-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const saveHandler = createSaveDraftHandler(repository);
    const resumeHandler = createResumeDraftHandler(repository);

    await saveHandler({
      type: "saveDraft",
      featureId: "001",
      story: "Test story",
      testCases: "Test cases",
    });

    const result = await resumeHandler({
      type: "resumeDraft",
      featureId: "001",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.equal(result.data.resumed, true);
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("resumeDraft handler fails for non-existent draft", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-handler-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const handler = createResumeDraftHandler(repository);

    const result = await handler({
      type: "resumeDraft",
      featureId: "non-existent",
    });

    assert.equal(result.success, false);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("getDraft handler returns draft correctly", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-handler-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const saveHandler = createSaveDraftHandler(repository);
    const getHandler = createGetDraftHandler(repository);

    await saveHandler({
      type: "saveDraft",
      featureId: "001",
      story: "Test story",
      testCases: "Test cases",
    });

    const result = await getHandler({
      type: "getDraft",
      featureId: "001",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.ok(result.data.draft);
      assert.equal(result.data.draft.featureId, "001");
      assert.equal(result.data.draft.story, "Test story");
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("getDraft handler returns null for non-existent draft", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-handler-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const handler = createGetDraftHandler(repository);

    const result = await handler({
      type: "getDraft",
      featureId: "non-existent",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.equal(result.data.draft, null);
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("listDrafts handler lists all drafts", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-handler-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const saveHandler = createSaveDraftHandler(repository);
    const listHandler = createListDraftsHandler(repository);

    await saveHandler({
      type: "saveDraft",
      featureId: "001",
      story: "Story 1",
      testCases: "Cases 1",
    });

    await saveHandler({
      type: "saveDraft",
      featureId: "002",
      story: "Story 2",
      testCases: "Cases 2",
    });

    const result = await listHandler({
      type: "listDrafts",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.equal(result.data.drafts.length, 2);
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
