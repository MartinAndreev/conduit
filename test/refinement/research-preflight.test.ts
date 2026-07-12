import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { createStartResearchRefinementHandler } from "@domains/refinement/handlers/start-research-refinement-handler.js";

test("research preflight saves a visible report before architect refinement", async () => {
  const projectRoot = await mkdtemp(
    path.join(os.tmpdir(), "conduit-research-"),
  );
  const featureDirectory = path.join(projectRoot, "specs", "001-example");
  const runDir = path.join(projectRoot, ".conduit", "runs", "research");
  const promptFile = path.join(runDir, "researcher.md");
  await mkdir(featureDirectory, { recursive: true });
  await mkdir(runDir, { recursive: true });
  try {
    const handler = createStartResearchRefinementHandler({
      projectRoot,
      builtinRoleRoot: "/roles",
      loadConfig: async () => ({
        version: 1,
        specsDir: "specs",
        stateDir: ".conduit",
        roles: {
          researcher: {
            runner: "opencode",
            mode: "subagent",
            readOnly: true,
            skill: { source: "file:.conduit/roles/researcher.md" },
          },
        },
      }),
      findFeature: async () => ({ id: "001", directory: featureDirectory }),
      planRun: async () => ({
        runDir,
        run: {
          id: "research",
          featureId: "001",
          status: "planned",
          createdAt: new Date().toISOString(),
          roles: [
            {
              name: "researcher",
              runner: "opencode",
              readOnly: true,
              owns: [],
              promptFile,
              prompt: "# Researcher",
              command: "opencode",
              args: [],
              skillSource: "file:researcher.md",
              status: "planned",
            },
          ],
        },
      }),
      executeRun: async () => [
        {
          role: "researcher",
          status: "completed",
          output: "## Confirmed facts\n\n- `src/auth.ts` owns login.",
        },
      ],
    });

    const result = await handler({
      type: "startResearchRefinement",
      featureId: "001",
      story: "Improve login feedback.",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.match(result.data.report, /Confirmed facts/);
      assert.equal(
        await readFile(result.data.reportFile, "utf8"),
        result.data.report,
      );
    }
    assert.match(await readFile(promptFile, "utf8"), /research assignment/i);
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
