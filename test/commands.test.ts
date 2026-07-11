import test from "node:test";
import assert from "node:assert/strict";
import { featureCommand } from "../src/commands/feature.js";
import { runCommand } from "../src/commands/run.js";

test("feature handler can be tested without Commander or filesystem access", async () => {
  const output: string[] = [];
  const feature = await featureCommand(
    "Add notes",
    { project: "/tmp/demo" },
    {
      output: (line: string) => output.push(line),
      progress: async (
        _text: string,
        work: (params?: {
          setText?: (text: string) => void;
        }) => Promise<{ id: string; directory: string; title: string }>,
      ) => work(),
      loadConfig: async () => ({ specsDir: "specs" }),
      createFeature: async ({ title }: { title: string }) => ({
        id: "007",
        directory: "/tmp/demo/specs/007-add-notes",
        title,
      }),
    },
  );
  assert.equal(feature.id, "007");
  assert.deepEqual(output as string[], [
    "Created feature 007 at /tmp/demo/specs/007-add-notes",
  ]);
});

test("run handler executes by default unless dry-run is requested", async () => {
  let receivedDryRun: boolean | undefined;
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
      progress: async (
        _text: string,
        work: (params?: {
          setText?: (text: string) => void;
        }) => Promise<unknown>,
      ) => work(),
      loadConfig: async () => ({}),
      planRun: async () => ({
        run: {
          id: "r1",
          featureId: "001",
          status: "running" as const,
          createdAt: new Date().toISOString(),
          roles: [],
        },
        runDir: "/tmp/run",
      }),
      executeRun: async ({ dryRun }: { dryRun?: boolean }) => {
        receivedDryRun = dryRun;
        return [];
      },
    },
  );
  assert.equal(receivedDryRun, false);
});
