import { test } from "bun:test";
import assert from "node:assert/strict";
import { formatWorkerRun } from "../src/tui/worker-run.js";

test("worker view retains a compact recent event list", () => {
  const view = formatWorkerRun({
    featureId: "001",
    roles: ["backend"],
    events: ["backend: queued", "backend: working"],
    status: "backend: working",
  });
  assert.match(view, /backend: working/);
  assert.match(view, /Ctrl\+C cancels/);
});

test("worker view nests a changed-file preview under its edit event", () => {
  const view = formatWorkerRun({
    featureId: "001",
    roles: ["backend"],
    events: [
      "backend: edited 1 file (+1 -1)\n└ src/version.js (+1 -1)\n  -old\n  +new",
    ],
    status: "backend: working",
  });
  assert.match(view, /• backend: edited 1 file \(\+1 -1\)/);
  assert.match(view, /└ src\/version\.js \(\+1 -1\)/);
  assert.match(view, /-old/);
  assert.match(view, /\+new/);
});
