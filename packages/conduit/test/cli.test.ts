import { test } from "bun:test";
import assert from "node:assert/strict";
import { createProgram } from "../src/cli.js";

test("Commander exposes Conduit commands", () => {
  const commands = createProgram().commands.map(
    (command: { name: () => string }) => command.name(),
  );
  assert.deepEqual(commands, [
    "init",
    "version",
    "storage-doctor",
    "feature",
    "refine",
    "roles",
    "role",
    "run",
    "status",
  ]);
  const run = createProgram().commands.find(
    (command) => command.name() === "run",
  );
  const flags = run?.options.map((option) => option.long);
  assert.ok(flags?.includes("--continue"));
  assert.ok(flags?.includes("--start-new"));
  assert.ok(flags?.includes("--confirm-discard-retained"));
});
