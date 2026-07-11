import test from "node:test";
import assert from "node:assert/strict";
import { featureCommand } from "../src/domains/features/handlers/feature-command.js";
import { runCommand } from "../src/domains/runs/handlers/run-command.js";

test("feature handler can be tested without Commander or filesystem access", async () => {
  const output: string[] = [];
  const feature = await featureCommand(
    "Add notes",
    { project: "/tmp/demo" },
    {
      output: (line: string) => output.push(line),
      progress: async <T>(
        _text: string,
        work: (params?: { setText?: (text: string) => void }) => Promise<T>,
      ) => work(),
      loadConfig: async () => ({
        version: 1,
        specsDir: "specs",
        stateDir: ".conduit",
        roles: {},
      }),
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
      progress: async <T>(
        _text: string,
        work: (params?: { setText?: (text: string) => void }) => Promise<T>,
      ) => work(),
      loadConfig: async () => ({
        version: 1,
        specsDir: "specs",
        stateDir: ".conduit",
        roles: {},
      }),
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
