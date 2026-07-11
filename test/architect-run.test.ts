import test from "node:test";
import assert from "node:assert/strict";
import { formatArchitectRun } from "../src/tui/architect-run.js";

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
