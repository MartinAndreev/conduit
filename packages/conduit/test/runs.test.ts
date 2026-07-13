import { test } from "bun:test";
import assert from "node:assert/strict";
import { commandForRole } from "../src/domains/runs/repositories/run-orchestrator.js";

test("builds subscription CLI commands for each supported runner", () => {
  assert.deepEqual(commandForRole({ runner: "opencode" }, "/tmp/task.md"), [
    "opencode",
    ["run", "Read /tmp/task.md and perform only your assigned task."],
  ]);
  assert.deepEqual(commandForRole({ runner: "codex" }, "/tmp/task.md"), [
    "codex",
    ["exec", "Read /tmp/task.md and perform only your assigned task."],
  ]);
  assert.deepEqual(commandForRole({ runner: "pi" }, "/tmp/task.md"), [
    "pi",
    ["-p", "Read /tmp/task.md and perform only your assigned task."],
  ]);
  assert.deepEqual(commandForRole({ runner: "kilo" }, "/tmp/task.md"), [
    "kilo",
    ["run", "Read /tmp/task.md and perform only your assigned task."],
  ]);
  assert.deepEqual(
    commandForRole(
      { runner: "opencode", model: "openai/gpt-5-mini" },
      "/tmp/task.md",
    ),
    [
      "opencode",
      [
        "run",
        "--model",
        "openai/gpt-5-mini",
        "Read /tmp/task.md and perform only your assigned task.",
      ],
    ],
  );
  assert.deepEqual(
    commandForRole({ runner: "codex", effort: "xhigh" }, "/tmp/task.md"),
    [
      "codex",
      [
        "exec",
        "Read /tmp/task.md and perform only your assigned task. Requested reasoning effort: xhigh.",
      ],
    ],
  );
});
