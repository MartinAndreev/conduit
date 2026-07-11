import { test } from "node:test";
import assert from "node:assert/strict";
import { createApplication } from "../../src/system/bootstrap/application.js";
import type { BootstrapDependencies } from "../../src/system/bootstrap/application.js";

function stubDeps(
  overrides: Partial<BootstrapDependencies> = {},
): BootstrapDependencies {
  return {
    loadConfig: async () => ({
      version: 1,
      specsDir: "specs",
      stateDir: ".conduit",
      roles: {},
    }),
    initializeProject: async () => ({
      createdConfig: true,
      configFile: "/tmp/proj/conduit.yml",
    }),
    createFeature: async ({ title }) => ({
      id: "001",
      directory: `/tmp/proj/specs/001-${title}`,
    }),
    findFeature: async ({ featureId }) => ({
      id: featureId,
      directory: `/tmp/proj/specs/${featureId}-demo`,
    }),
    planRun: async () => ({
      run: {
        id: "r1",
        featureId: "001",
        status: "planned" as const,
        createdAt: new Date().toISOString(),
        roles: [],
      },
      runDir: "/tmp/runs/r1",
    }),
    latestRuns: async () => [],
    ...overrides,
  };
}

interface InitResult {
  createdConfig: boolean;
  configFile: string;
}

interface BootstrapState {
  initialized: boolean;
  configPath?: string;
}

interface RunEntry {
  id: string;
  featureId: string;
  status: string;
  createdAt: string;
  roles: unknown[];
}

test("createApplication returns commandBus and queryBus", () => {
  const app = createApplication(stubDeps());
  assert.ok(app.commandBus);
  assert.ok(app.queryBus);
});

test("initializeProject command dispatches to dependency", async () => {
  const app = createApplication(stubDeps());
  const result = await app.commandBus.dispatch({
    type: "initializeProject",
    projectRoot: "/tmp/proj",
    templateRoot: "/tmp/tpl",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as InitResult;
    assert.equal(data.createdConfig, true);
  }
});

test("projectBootstrapState query returns initialized when config loads", async () => {
  const app = createApplication(stubDeps());
  const result = await app.queryBus.execute({
    type: "projectBootstrapState",
    projectRoot: "/tmp/proj",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as BootstrapState;
    assert.equal(data.initialized, true);
    assert.match(data.configPath!, /conduit\.yml/);
  }
});

test("projectBootstrapState query returns uninitialized on config error", async () => {
  const app = createApplication(
    stubDeps({
      loadConfig: async () => {
        throw new Error("No conduit.yml found");
      },
    }),
  );
  const result = await app.queryBus.execute({
    type: "projectBootstrapState",
    projectRoot: "/tmp/proj",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as BootstrapState;
    assert.equal(data.initialized, false);
  }
});

test("latestRuns query returns runs from dependency", async () => {
  const runs = [
    {
      id: "r1",
      featureId: "001",
      status: "completed" as const,
      createdAt: "2025-01-01T00:00:00Z",
      roles: [],
    },
  ];
  const app = createApplication(stubDeps({ latestRuns: async () => runs }));
  const result = await app.queryBus.execute({
    type: "latestRuns",
    projectRoot: "/tmp/proj",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as RunEntry[];
    assert.equal(data.length, 1);
    assert.equal(data[0].id, "r1");
  }
});

test("unregistered command returns HANDLER_NOT_FOUND", async () => {
  const app = createApplication(stubDeps());
  const result = await app.commandBus.dispatch({
    type: "nonexistent",
  });
  assert.equal(result.success, false);
  if (!result.success) {
    assert.equal(result.error.code, "HANDLER_NOT_FOUND");
  }
});

test("unregistered query returns HANDLER_NOT_FOUND", async () => {
  const app = createApplication(stubDeps());
  const result = await app.queryBus.execute({
    type: "nonexistent",
  });
  assert.equal(result.success, false);
  if (!result.success) {
    assert.equal(result.error.code, "HANDLER_NOT_FOUND");
  }
});
