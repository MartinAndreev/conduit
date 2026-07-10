import test from "node:test";
import assert from "node:assert/strict";
import { createProgram } from "../src/cli.js";

test("Commander exposes Conduit commands", () => {
  const commands = createProgram().commands.map((command) => command.name());
  assert.deepEqual(commands, [
    "init",
    "version",
    "feature",
    "refine",
    "roles",
    "role",
    "run",
    "status",
  ]);
});
