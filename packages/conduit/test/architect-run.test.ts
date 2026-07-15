import { test } from "bun:test";
import assert from "node:assert/strict";
import { formatArchitectRun } from "../src/tui/architect-run.js";
import {
  architectActivitySummary,
  architectCurrentActivity,
  architectRunningStatus,
} from "../src/tui/helpers/architect-activity-presentation.js";

test("live architect view keeps transcript contents collapsed", () => {
  const view = formatArchitectRun({
    featureId: "001",
    transcript: "exec\npnpm test\nverbose line one\nverbose line two\n",
  });
  assert.match(view, /• Ran pnpm test/);
  assert.doesNotMatch(view, /verbose line one/);
});

test("completed architect view identifies completion before dashboard handoff", () => {
  const view = formatArchitectRun({
    featureId: "001",
    transcript: "exec\npnpm test\nverbose line one\n",
    completed: true,
  });
  assert.doesNotMatch(view, /verbose line one/);
  assert.match(view, /Status: refinement completed/);
});

test("architect activity presentation derives dynamic status copy", () => {
  assert.equal(architectCurrentActivity("007"), "Refining feature 007");
  assert.equal(
    architectCurrentActivity("007", "Running tests"),
    "Running tests",
  );
  assert.equal(
    architectRunningStatus("12:34:56"),
    "Process is still running · last structured output 12:34:56",
  );
  assert.equal(architectActivitySummary(2, 5), "Changed files: 2 | Events: 5");
});
