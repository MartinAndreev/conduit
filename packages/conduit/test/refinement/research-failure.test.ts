import assert from "node:assert/strict";
import { test } from "bun:test";
import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import { formatResearchFailure } from "@tui/helpers/research-failure.js";

test("research failure reports the structured launch cause and remediation", () => {
  const events: RunnerEvent[] = [
    {
      type: "error",
      runId: "run-17",
      roleId: "researcher",
      timestamp: "2026-07-15T10:00:00.000Z",
      payload: {
        kind: "error",
        code: "PROCESS_ERROR",
        message: "spawn opencode ENOENT",
        recoverable: false,
      },
    },
    {
      type: "lifecycle",
      runId: "run-17",
      roleId: "researcher",
      timestamp: "2026-07-15T10:00:01.000Z",
      payload: {
        kind: "lifecycle",
        state: "failed",
        message: "researcher: failed to start",
      },
    },
  ];

  const message = formatResearchFailure(events, "run-17");

  assert.match(message, /PROCESS_ERROR/);
  assert.match(message, /spawn opencode ENOENT/);
  assert.match(message, /executable is on PATH/);
  assert.match(message, /Run ID: run-17/);
  assert.doesNotMatch(message, /researcher: failed to start/);
});

test("research failure includes bounded stderr and exit code", () => {
  const events: RunnerEvent[] = [
    {
      type: "tool-output",
      runId: "run-18",
      roleId: "researcher",
      timestamp: "2026-07-15T10:00:00.000Z",
      payload: {
        kind: "tool-output",
        tool: "runner stderr",
        output: "Authentication failed for the configured runner.",
        truncated: false,
      },
    },
    {
      type: "result",
      runId: "run-18",
      roleId: "researcher",
      timestamp: "2026-07-15T10:00:01.000Z",
      payload: {
        kind: "result",
        exitCode: 1,
        files: [],
        summary: "researcher: failed",
      },
    },
  ];

  const message = formatResearchFailure(events, "run-18");

  assert.match(message, /exit code 1/);
  assert.match(message, /Runner stderr:/);
  assert.match(message, /Authentication failed/);
});

test("research worktree hook failures are not reported as missing runners", () => {
  const events: RunnerEvent[] = [
    {
      type: "error",
      runId: "run-19",
      roleId: "researcher",
      timestamp: "2026-07-15T10:00:00.000Z",
      payload: {
        kind: "error",
        code: "ROLE_LAUNCH_FAILED",
        message:
          'Could not create worktree for researcher: error Command "run-if-changed" not found. husky - post-checkout hook exited with code 1',
        recoverable: false,
      },
    },
  ];

  const message = formatResearchFailure(events, "run-19");

  assert.match(message, /checkout hook failed/);
  assert.match(message, /post-checkout\/Husky hook/);
  assert.doesNotMatch(message, /researcher runner was not found/);
});
