import { describe, expect, test } from "bun:test";
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

describe("role execution stages", () => {
  test("follow configured dependencies", () => {
    const stages = roleExecutionStages([
      role("frontend"),
      role("backend"),
      role("qa", ["frontend", "backend"]),
      role("documentation", ["frontend", "backend"]),
      role("reviewer", ["qa", "documentation"]),
    ]);
    expect(stages.map((stage) => stage.map((item) => item.name))).toEqual([
      ["frontend", "backend"],
      ["qa", "documentation"],
      ["reviewer"],
    ]);
  });

  test("ignore dependencies outside selected roles", () => {
    const stages = roleExecutionStages([
      role("frontend"),
      role("qa", ["frontend", "backend"]),
    ]);
    expect(stages.map((stage) => stage.map((item) => item.name))).toEqual([
      ["frontend"],
      ["qa"],
    ]);
  });

  test("reject dependency cycles", () => {
    expect(() =>
      roleExecutionStages([role("a", ["b"]), role("b", ["a"])]),
    ).toThrow(/dependency cycle/i);
  });
});
