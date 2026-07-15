import { test } from "bun:test";
import assert from "node:assert/strict";
import { createApplication } from "../../src/system/bootstrap/application.js";
import type { BootstrapDependencies } from "../../src/system/bootstrap/application.js";
import type { ApplicationBootstrapService } from "../../src/system/bootstrap/application.js";
import { InMemoryCredentialStore } from "../../src/domains/credentials/repositories/in-memory-store.js";
import { LocalSpecKitProvider } from "../../src/domains/features/providers/local-spec-kit-provider.js";
import { createPortraitRegistry } from "../../src/domains/roles/repositories/portrait-registry.js";
import { createConfigurationRepository } from "../../src/domains/configuration/repositories/configuration-repository.js";
import { UpdatesBootstrapService } from "../../src/domains/updates/services/updates-bootstrap-service.js";
import { InstallationKind } from "../../src/domains/updates/enums/installation-kind.js";
import { UpdateStatus } from "../../src/domains/updates/enums/update-status.js";

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
    configurationRepository: createConfigurationRepository(),
    credentialStore: new InMemoryCredentialStore(),
    featureProvider: new LocalSpecKitProvider("/tmp/specs"),
    portraitRegistry: createPortraitRegistry(),
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

test("createApplication composes registrations through the bootstrap contract", async () => {
  const service: ApplicationBootstrapService = {
    register({ queryBus, repositories }) {
      assert.ok(repositories.runEvents);
      queryBus.register("bootstrapContractProbe", async () => ({
        success: true,
        data: { registered: true },
      }));
    },
  };
  const app = createApplication(stubDeps(), [service]);
  const result = await app.queryBus.execute({ type: "bootstrapContractProbe" });
  assert.equal(result.success, true);
  if (result.success)
    assert.equal((result.data as { registered: boolean }).registered, true);
});

test("UpdatesBootstrapService registers update query and command contracts", async () => {
  const release = {
    version: "0.6.0",
    tagName: "v0.6.0",
    publishedAt: "2026-07-15T08:00:00Z",
    releaseUrl: "https://github.com/MartinAndreev/conduit/releases/tag/v0.6.0",
    assets: [],
  };
  let installed = false;
  const app = createApplication(stubDeps(), [
    new UpdatesBootstrapService(
      { discover: async () => release },
      {
        install: async () => {
          installed = true;
        },
      },
      "0.5.4",
    ),
  ]);

  const checked = await app.queryBus.execute({ type: "checkForUpdate" });
  assert.equal(
    checked.success &&
      (checked.data as { status: UpdateStatus }).status ===
        UpdateStatus.Available,
    true,
  );

  const updated = await app.commandBus.dispatch({
    type: "startUpdate",
    release,
    installation: {
      kind: InstallationKind.Standalone,
      automatic: true,
      label: "Official standalone binary",
    },
  });
  assert.equal(installed, true);
  assert.equal(
    updated.success &&
      (updated.data as { status: UpdateStatus }).status ===
        UpdateStatus.Succeeded,
    true,
  );
  await app.close();
});

test("project bootstrap injects the source-version repository", async () => {
  let sourceVersionsAvailable = false;
  const service: ApplicationBootstrapService = {
    register({ repositories }) {
      sourceVersionsAvailable = Boolean(repositories.sourceVersions);
    },
  };
  const app = createApplication(
    stubDeps({ projectRoot: "/tmp/conduit-source-bootstrap" }),
    [service],
  );

  assert.equal(sourceVersionsAvailable, true);
  await app.close();
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

test("listFeatures query returns features from provider", async () => {
  const app = createApplication(stubDeps());
  const result = await app.queryBus.execute({
    type: "listFeatures",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as { features: unknown[] };
    assert.ok(Array.isArray(data.features));
  }
});

test("listPortraits query returns portraits from registry", async () => {
  const app = createApplication(stubDeps());
  const result = await app.queryBus.execute({
    type: "listPortraits",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as { portraits: unknown[] };
    assert.ok(Array.isArray(data.portraits));
    assert.ok(data.portraits.length > 0);
  }
});

test("setCredential command dispatches to credential store", async () => {
  const app = createApplication(stubDeps());
  const result = await app.commandBus.dispatch({
    type: "setCredential",
    profile: "test",
    key: "apiKey",
    value: "secret123",
  });
  assert.equal(result.success, true);
});

test("getCredential query returns value from credential store", async () => {
  const store = new InMemoryCredentialStore();
  await store.set("test", "apiKey", "secret123");
  const app = createApplication(stubDeps({ credentialStore: store }));
  const result = await app.queryBus.execute({
    type: "getCredential",
    profile: "test",
    key: "apiKey",
  });
  assert.equal(result.success, true);
  if (result.success) {
    const data = result.data as { value: string | undefined };
    assert.equal(data.value, "secret123");
  }
});

test("updateFeatureMetadata command dispatches to provider", async () => {
  const app = createApplication(stubDeps());
  const result = await app.commandBus.dispatch({
    type: "updateFeatureMetadata",
    featureId: "001-test",
    lifecycle: "in_progress",
  });
  assert.equal(result.success, true);
});
