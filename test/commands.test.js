import test from "node:test";
import assert from "node:assert/strict";
import { featureCommand } from "../src/commands/feature.js";
import { runCommand } from "../src/commands/run.js";

test("feature handler can be tested without Commander or filesystem access", async () => {
  const output = [];
  const feature = await featureCommand(
    "Add notes",
    { project: "/tmp/demo" },
    {
      output: (line) => output.push(line),
      progress: async (_text, work) => work(),
      loadConfig: async () => ({ specsDir: "specs" }),
      createFeature: async ({ title }) => ({
        id: "007",
        directory: "/tmp/demo/specs/007-add-notes",
        title,
      }),
    },
  );
  assert.equal(feature.id, "007");
  assert.deepEqual(output, [
    "Created feature 007 at /tmp/demo/specs/007-add-notes",
  ]);
});

test("run handler executes by default unless dry-run is requested", async () => {
  let receivedDryRun;
  await runCommand(
    "001",
    {
      project: "/tmp/demo",
      roles: "frontend,backend",
      dryRun: false,
      execute: false,
      fetchSkills: false,
    },
    {
      output: () => {},
      progress: async (_text, work) => work(),
      builtinRoot: "/tmp/skills",
      loadConfig: async () => ({}),
      planRun: async () => ({ run: {}, runDir: "/tmp/run" }),
      executeRun: async ({ dryRun }) => {
        receivedDryRun = dryRun;
        return [];
      },
    },
  );
  assert.equal(receivedDryRun, false);
});
