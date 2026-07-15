import { test } from "bun:test";
import assert from "node:assert/strict";
import { formatArchitectRun } from "../src/tui/architect-run.js";
import {
  architectActivitySummary,
  architectCurrentActivity,
  architectRunningStatus,
} from "../src/tui/helpers/architect-activity-presentation.js";
import type { ArchitectEvent } from "../src/domains/refinement/types/architect-event.js";

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
    architectCurrentActivity("007", {
      type: "activity",
      timestamp: "2026-01-01T00:00:00.000Z",
      content: "Running tests",
    }),
    "Running tests",
  );
  assert.equal(
    architectRunningStatus("12:34:56"),
    "Process is still running · last structured output 12:34:56",
  );
  assert.equal(architectActivitySummary(2, 5), "Changed files: 2 | Events: 5");
});

test("architect loader keeps raw event logs out of its status message", () => {
  const command: ArchitectEvent = {
    type: "tool-call",
    timestamp: "2026-01-01T00:00:00.000Z",
    content: "/usr/bin/zsh -lc 'sed -n 1,260p package.json && git status'",
  };
  assert.equal(architectCurrentActivity("007", command), "Running a command");
  assert.doesNotMatch(architectCurrentActivity("007", command), /zsh|sed|git/);

  const longReasoning: ArchitectEvent = {
    type: "thought",
    timestamp: "2026-01-01T00:00:00.000Z",
    content: `Inspecting   repository\n${"context ".repeat(30)}`,
  };
  const message = architectCurrentActivity("007", longReasoning);
  assert.equal(message.includes("\n"), false);
  assert.ok(message.length <= 120);
});
