import assert from "node:assert/strict";
import { test } from "bun:test";
import {
  refinementApprovalRoute,
  refinementResumeView,
} from "@tui/helpers/refinement-resume.js";

test("persisted research resumes after an architect failure", () => {
  assert.equal(
    refinementResumeView({
      hasPacket: true,
      hasResearch: true,
      revisionStatus: "failed",
    }),
    "researchReview",
  );
  assert.equal(
    refinementResumeView({
      hasPacket: true,
      hasResearch: true,
    }),
    "researchReview",
  );
});

test("available research does not imply a research rerun", () => {
  assert.equal(
    refinementApprovalRoute({
      architectRequested: true,
      researchRerunRequested: false,
    }),
    "architect",
  );
  assert.equal(
    refinementApprovalRoute({
      architectRequested: true,
      researchRerunRequested: true,
    }),
    "research",
  );
});

test("persisted architect decisions take priority over research review", () => {
  assert.equal(
    refinementResumeView({
      hasPacket: true,
      hasResearch: true,
      revisionStatus: "awaiting_clarification",
    }),
    "clarifications",
  );
  assert.equal(
    refinementResumeView({
      hasPacket: true,
      hasResearch: true,
      revisionStatus: "ready_for_review",
    }),
    "review",
  );
  assert.equal(
    refinementResumeView({
      hasPacket: true,
      hasResearch: true,
      revisionStatus: "approved",
    }),
    "packet",
  );
});
