import test from "node:test";
import assert from "node:assert/strict";
import { roleExecutionStages } from "../src/domains/runs/repositories/run-orchestrator.js";
import type { RunRole } from "../src/domains/runs/types/run.js";

function role(name: string, dependsOn: string[] = []): RunRole {
  return {
    name,
    runner: "opencode",
    readOnly: false,
    owns: [],
    dependsOn,
    promptFile: "",
    prompt: "",
    command: "true",
    args: [],
    skillSource: "",
    status: "planned",
  };
}

test("role execution stages follow configured dependencies", () => {
  const stages = roleExecutionStages([
    role("frontend"),
    role("backend"),
    role("qa", ["frontend", "backend"]),
    role("documentation", ["frontend", "backend"]),
    role("reviewer", ["qa", "documentation"]),
  ]);
  assert.deepEqual(
    stages.map((stage) => stage.map((item) => item.name)),
    [["frontend", "backend"], ["qa", "documentation"], ["reviewer"]],
  );
});

test("role execution stages ignore dependencies outside selected roles", () => {
  const stages = roleExecutionStages([
    role("frontend"),
    role("qa", ["frontend", "backend"]),
  ]);
  assert.deepEqual(
    stages.map((stage) => stage.map((item) => item.name)),
    [["frontend"], ["qa"]],
  );
});

test("role execution stages reject dependency cycles", () => {
  assert.throws(
    () => roleExecutionStages([role("a", ["b"]), role("b", ["a"])]),
    /dependency cycle/i,
  );
});
