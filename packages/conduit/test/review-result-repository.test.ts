import { test } from "bun:test";
import assert from "node:assert/strict";
import { InMemoryReviewResultRepository } from "../src/domains/runs/repositories/in-memory-review-result-repository.js";

test("save and load persist review by runId", async () => {
  const repo = new InMemoryReviewResultRepository();
  await repo.save({
    reviewId: "rev-1",
    runId: "r1",
    featureId: "001",
    decision: "approved",
    findings: [],
    evidencePaths: [],
    followUp: undefined,
    reviewedAt: "2026-01-01T00:00:00Z",
  });

  const result = await repo.load("r1");
  assert.ok(result);
  assert.equal(result!.reviewId, "rev-1");
  assert.equal(result!.decision, "approved");
});

test("load returns undefined for unknown runId", async () => {
  const repo = new InMemoryReviewResultRepository();
  const result = await repo.load("unknown");
  assert.equal(result, undefined);
});

test("save overwrites previous review for same runId", async () => {
  const repo = new InMemoryReviewResultRepository();
  await repo.save({
    reviewId: "rev-1",
    runId: "r1",
    featureId: "001",
    decision: "approved",
    findings: [],
    evidencePaths: [],
    followUp: undefined,
    reviewedAt: "2026-01-01",
  });
  await repo.save({
    reviewId: "rev-2",
    runId: "r1",
    featureId: "001",
    decision: "rejected",
    findings: [{ severity: "error", message: "issue" }],
    evidencePaths: ["src/foo.ts"],
    followUp: "fix it",
    reviewedAt: "2026-01-02",
  });

  const result = await repo.load("r1");
  assert.ok(result);
  assert.equal(result!.reviewId, "rev-2");
  assert.equal(result!.decision, "rejected");
  assert.equal(result!.findings.length, 1);
});
