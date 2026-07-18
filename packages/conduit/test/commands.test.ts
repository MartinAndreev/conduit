import { test } from "bun:test";
import assert from "node:assert/strict";
import { featureCommand } from "../src/domains/features/handlers/feature-command.js";
import { runCommand } from "../src/domains/runs/handlers/run-command.js";
import { RoleWorkspaceState } from "../src/domains/runs/enums/role-workspace-state.js";
import type { RoleWorkspaceRepository } from "../src/domains/runs/interfaces/role-workspace-repository.js";
import type { Run } from "../src/domains/runs/types/run.js";

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

test("CLI run handler dispatches the shared start-feature-run command", async () => {
  let dispatched: Record<string, unknown> | undefined;
  let planned = false;
  await runCommand(
    "009",
    {
      project: "/tmp/demo",
      roles: "worker,reviewer",
      startNew: true,
      confirmDiscardRetained: true,
      dryRun: true,
      fetchSkills: true,
    },
    {
      loadConfig: async () => ({
        version: 1,
        specsDir: "specs",
        stateDir: ".conduit",
        roles: {},
      }),
      planRun: async () => {
        planned = true;
        throw new Error("CLI must dispatch instead of planning directly");
      },
      startFeatureRun: async (command) => {
        dispatched = { ...command };
        return { runId: "shared-run", results: [] };
      },
    },
  );
  assert.equal(planned, false);
  assert.deepEqual(dispatched, {
    type: "startFeatureRun",
    featureId: "009",
    roleNames: ["worker", "reviewer"],
    mode: "start-new",
    confirmDiscardRetained: true,
    waitForCompletion: true,
    dryRun: true,
    fetchSkills: true,
  });
});

test("run handler Continue resumes retained work without duplicate planning", async () => {
  let planned = false;
  let resumed = "";
  await runCommand(
    "009",
    { project: "/tmp/demo", roles: "reviewer", continue: true },
    {
      loadConfig: async () => ({
        version: 1,
        specsDir: "specs",
        stateDir: ".conduit",
        roles: {},
      }),
      planRun: async () => {
        planned = true;
        throw new Error("must not plan");
      },
      getWorkspaceContinuity: async () => ({
        state: "compatible-continue",
        runId: "retained-run",
        roles: ["reviewer"],
        preservedRoles: ["worker"],
        retryRoles: ["reviewer"],
      }),
      resumeRun: async (runId) => {
        resumed = runId;
        return {
          id: runId,
          featureId: "009",
          status: "completed",
          createdAt: new Date().toISOString(),
          roles: [],
        };
      },
    },
  );
  assert.equal(planned, false);
  assert.equal(resumed, "retained-run");
});

test("run handler requires explicit Start Anew confirmation", async () => {
  await assert.rejects(
    () =>
      runCommand(
        "009",
        { project: "/tmp/demo", roles: "reviewer", startNew: true },
        {
          loadConfig: async () => ({
            version: 1,
            specsDir: "specs",
            stateDir: ".conduit",
            roles: {},
          }),
          getWorkspaceContinuity: async () => ({
            state: "incompatible-retained",
            runId: "retained-run",
            runIds: ["retained-run"],
            roles: ["reviewer"],
            reason: "package changed",
          }),
        },
      ),
    /confirm-discard-retained/,
  );
});

test("run handler releases advanced Start Anew slots when snapshot persistence fails", async () => {
  const role: Run["roles"][number] = {
    name: "frontend",
    runner: "codex",
    readOnly: true,
    owns: [],
    dependsOn: [],
    promptFile: "",
    prompt: "",
    command: "",
    args: [],
    skillSource: "test",
    status: "planned" as const,
    workspaceRepositoryId: "repo",
    workspaceRoleKey: "frontend",
    workspaceBranchName: "conduit/new/frontend",
    workspaceAssignmentHash: "a".repeat(64),
    workspaceLeaseOwner: "new:frontend",
    worktree: "/tmp/slot/frontend",
  };
  const run: Run = {
    id: "new",
    featureId: "009",
    status: "planned",
    createdAt: new Date().toISOString(),
    startingHead: "b".repeat(40),
    featurePackageHash: "c".repeat(64),
    roles: [role],
  };
  let saves = 0;
  let retained = false;
  const slot = {
    repositoryId: "repo",
    roleKey: "frontend",
    generation: 2,
    workspacePath: role.worktree!,
    owningRunId: run.id,
    state: RoleWorkspaceState.Provisioning,
    startingHead: run.startingHead!,
    packageHash: run.featurePackageHash!,
    assignmentHash: role.workspaceAssignmentHash!,
    branchName: role.workspaceBranchName!,
    leaseOwner: role.workspaceLeaseOwner!,
    fencingToken: 2,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  };
  const workspaces = {
    load: async () => slot,
    claim: async () => {
      throw new Error("unused");
    },
    claimAll: async () => {
      throw new Error("unused");
    },
    advanceAll: async () => {
      throw new Error("unused");
    },
    recordHead: async () => false,
    retain: async () => {
      retained = true;
      return true;
    },
    transition: async () => false,
    completeGeneration: async () => false,
    remove: async () => false,
    listByRun: async () => [slot],
    listCleanupCandidates: async () => [],
    listGenerations: async () => [],
  } satisfies RoleWorkspaceRepository;

  await assert.rejects(
    () =>
      runCommand(
        "009",
        {
          project: "/tmp/demo",
          roles: "frontend",
          startNew: true,
          confirmDiscardRetained: true,
        },
        {
          output: () => {},
          progress: async <T>(_text: string, work: () => Promise<T>) => work(),
          loadConfig: async () => ({
            version: 1,
            specsDir: "specs",
            stateDir: ".conduit",
            roles: {},
          }),
          planRun: async () => ({ run, runDir: "/tmp/run" }),
          getWorkspaceContinuity: async () => ({
            state: "incompatible-retained",
            runId: "old",
            runIds: ["old"],
            roles: ["frontend"],
            reason: "incompatible",
          }),
          prepareStartNew: async () => {
            role.workspaceFencingToken = 2;
          },
          runRecoveryRepository: {
            saveSnapshot: async () => {
              saves += 1;
              if (saves === 2) throw new Error("snapshot failed");
              return {
                run,
                state: "planned",
                version: 1,
                updatedAt: new Date().toISOString(),
              };
            },
            loadSnapshot: async () => undefined,
            listSnapshots: async () => [],
            claimFailedRun: async () => undefined,
            markInterrupted: async () => {},
            markCancelled: async () => {},
          },
          roleWorkspaceRepository: workspaces,
        },
      ),
    /snapshot failed/,
  );
  assert.equal(retained, true);
});

test("run handler persists role workspaces while the run is active", async () => {
  const expectedVersions: Array<number | undefined> = [];
  let version = 0;
  const run = {
    id: "r-live",
    featureId: "001",
    status: "running" as const,
    createdAt: new Date().toISOString(),
    roles: [],
  };
  await runCommand(
    "001",
    {
      project: "/tmp/demo",
      roles: "frontend",
      dryRun: false,
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
      planRun: async () => ({ run, runDir: "/tmp/run" }),
      executeRun: async ({
        onRoleWorkspaceReady,
      }: {
        onRoleWorkspaceReady?: () => Promise<void>;
      }) => {
        await onRoleWorkspaceReady?.();
        return [];
      },
      runRecoveryRepository: {
        claimFailedRun: async () => undefined,
        saveSnapshot: async (savedRun, expectedVersion) => {
          expectedVersions.push(expectedVersion);
          version += 1;
          return {
            run: savedRun,
            state: "running",
            version,
            updatedAt: new Date().toISOString(),
          };
        },
        loadSnapshot: async () => undefined,
        listSnapshots: async () => [],
        markInterrupted: async () => {},
        markCancelled: async () => {},
      },
    },
  );

  assert.deepEqual(expectedVersions, [undefined, 1, 2]);
});
