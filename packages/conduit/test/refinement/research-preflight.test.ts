import { test } from "bun:test";
import assert from "node:assert/strict";
import {
  mkdtemp,
  mkdir,
  readFile,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { createStartResearchRefinementHandler } from "@domains/refinement/handlers/start-research-refinement-handler.js";
import { createAgentAssignmentV1 } from "@domains/runs/factories/agent-assignment-factory.js";

test("research preflight saves a visible report before architect refinement", async () => {
  const projectRoot = await mkdtemp(
    path.join(os.tmpdir(), "conduit-research-"),
  );
  const featureDirectory = path.join(projectRoot, "specs", "001-example");
  const runDir = path.join(projectRoot, ".conduit", "runs", "research");
  const promptFile = path.join(runDir, "researcher-assignment.json");
  const contextFile = path.join(runDir, "researcher-context.md");
  await mkdir(featureDirectory, { recursive: true });
  await mkdir(path.join(projectRoot, "src"), { recursive: true });
  await writeFile(path.join(projectRoot, "src", "auth.ts"), "export {};\n");
  await mkdir(runDir, { recursive: true });
  try {
    let savedReport: string | undefined;
    let isolatedWorkspace = "";
    const snapshotStatuses: string[] = [];
    let signalReportSaved: () => void = () => {};
    const reportSaved = new Promise<void>((resolve) => {
      signalReportSaved = resolve;
    });
    const handler = createStartResearchRefinementHandler({
      projectRoot,
      builtinRoleRoot: "/roles",
      loadConfig: async () => ({
        version: 1,
        specsDir: "specs",
        stateDir: ".conduit",
        roles: {
          researcher: {
            runner: "codex",
            mode: "subagent",
            readOnly: true,
            skill: { source: "file:.conduit/roles/researcher.md" },
          },
        },
      }),
      findFeature: async () => ({ id: "001", directory: featureDirectory }),
      planRun: async (params) => {
        assert.equal(params.sharedReadOnlyWorkspace, true);
        return {
          runDir,
          run: {
            id: "research",
            featureId: "001",
            status: "planned",
            createdAt: new Date().toISOString(),
            roles: [
              {
                name: "researcher",
                runner: "codex",
                readOnly: false,
                owns: [],
                dependsOn: [],
                promptFile,
                prompt: "{}",
                context: "# Researcher",
                contextFile,
                command: "codex",
                args: ["exec", "Read the researcher prompt."],
                skillSource: "file:researcher.md",
                status: "planned",
                assignment: createAgentAssignmentV1({
                  assignmentId: "research:researcher",
                  role: "researcher",
                  roleKind: "research",
                  objective: "Research the feature.",
                  ownedPaths: [],
                  contextReferences: [path.relative(projectRoot, contextFile)],
                  acceptanceCriteria: ["Return evidence."],
                  contracts: ["specs"],
                }),
              },
            ],
          },
        };
      },
      executeRun: async ({ run, sharedReadOnlyWorkspace }) => {
        assert.equal(sharedReadOnlyWorkspace, true);
        isolatedWorkspace = run.roles[0]?.worktree ?? "";
        assert.notEqual(isolatedWorkspace, projectRoot);
        assert.equal(
          await readFile(
            path.join(isolatedWorkspace, "src", "auth.ts"),
            "utf8",
          ),
          "export {};\n",
        );
        await writeFile(
          path.join(isolatedWorkspace, "agent-change.txt"),
          "isolated\n",
        );
        assert.equal(
          run.roles[0]?.readOnly,
          true,
          "research preflight must never create a writable worktree",
        );
        assert.deepEqual(run.roles[0]?.args, [
          "exec",
          "Read the researcher prompt.",
        ]);
        assert.equal(
          run.roles[0]?.finalOutputFile,
          path.join(runDir, "researcher-agent-response.json"),
        );
        assert.doesNotMatch(
          run.roles[0]?.prompt ?? "",
          /Write the final Markdown report to/,
        );
        await new Promise((resolve) => setTimeout(resolve, 5));
        run.status = "completed";
        run.roles[0]!.status = "completed";
        return [
          {
            role: "researcher",
            status: "completed",
            stdout:
              '{"protocolVersion":"1.0","status":"completed","summary":"Researched auth context.","verdict":null,"artifacts":[],"findings":[{"severity":"info","category":"fact","message":"src/auth.ts owns login.","path":"src/auth.ts","evidence":["src/auth.ts"]}],"verification":[],"decisions":[],"blockers":[],"questions":[],"risks":[],"evidence":[{"kind":"path","reference":"src/auth.ts"}],"memoryProposals":[],"globalPromotionProposals":[]}',
          },
        ];
      },
      eventRepository: {
        append: async () => {},
        loadByRun: async () => [],
        loadByRole: async () => [],
        loadRoleIds: async () => [],
        clear: async () => {},
      },
      processRegistry: {
        register: () => {},
        get: () => undefined,
        getByRun: () => [],
        cancel: () => false,
        remove: () => {},
      },
      reportRepository: {
        save: async (featureId, report) => {
          savedReport = report;
          signalReportSaved();
          return {
            featureId,
            report,
            updatedAt: new Date().toISOString(),
            version: 1,
          };
        },
        load: async () => undefined,
      },
      recoveryRepository: {
        claimFailedRun: async () => undefined,
        saveSnapshot: async (run) => {
          snapshotStatuses.push(run.status);
          return {
            run,
            state: run.status === "completed" ? "complete" : "planned",
            version: 1,
            updatedAt: new Date().toISOString(),
          };
        },
        loadSnapshot: async () => undefined,
        listSnapshots: async () => [],
        markInterrupted: async () => {},
        markCancelled: async () => {},
      },
    });

    const result = await handler({
      type: "startResearchRefinement",
      featureId: "001",
      story: "Improve login feedback.",
    });

    assert.equal(result.success, true);
    if (result.success) {
      assert.equal(result.data.runId, "research");
      await reportSaved;
      assert.equal(result.data.reportFile, "conduit://research/001");
      assert.match(savedReport ?? "", /Findings/);
    }
    for (let attempt = 0; attempt < 20; attempt += 1) {
      if (!(await stat(isolatedWorkspace).catch(() => undefined))) break;
      await new Promise((resolve) => setTimeout(resolve, 5));
    }
    assert.equal(
      await readFile(path.join(projectRoot, "agent-change.txt"), "utf8").catch(
        () => undefined,
      ),
      undefined,
    );
    assert.equal(
      await readFile(
        path.join(isolatedWorkspace, "agent-change.txt"),
        "utf8",
      ).catch(() => undefined),
      undefined,
    );
    assert.deepEqual(snapshotStatuses, ["planned", "completed"]);
    assert.match(await readFile(promptFile, "utf8"), /"roleKind": "research"/);
    assert.match(await readFile(contextFile, "utf8"), /AgentResponseV1/);
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
