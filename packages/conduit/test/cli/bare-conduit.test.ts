import { test } from "bun:test";
import assert from "node:assert/strict";
import { tmpdir } from "node:os";
import path from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { handleBareConduit } from "../../src/cli.js";
import type { PromptFn } from "../../src/helpers/prompt.js";
import { UpdatesBootstrapService } from "../../src/domains/updates/services/updates-bootstrap-service.js";
import { GitHubReleaseDiscovery } from "../../src/domains/updates/repositories/github-release-discovery.js";

async function setupGitRepo(): Promise<string> {
  const tempDir = await mkdtemp(path.join(tmpdir(), "conduit-test-"));
  execFileSync("git", ["init"], { cwd: tempDir, stdio: "ignore" });
  return tempDir;
}

test("bare conduit in uninitialized repo offers initialization and accepts", async () => {
  const tempDir = await setupGitRepo();
  try {
    const messages: string[] = [];
    const promptResponses = ["y"];
    const mockPrompt: PromptFn = async () => promptResponses.shift()!;
    let homeStarted = false;

    await handleBareConduit(tempDir, {
      prompt: mockPrompt,
      output: (msg) => messages.push(msg),
      startHome: async () => {
        homeStarted = true;
      },
      startupMigration: async () => {},
      environment: { XDG_DATA_HOME: tempDir },
    });

    assert.ok(
      messages.some((m) => m.includes("Conduit initialized")),
      `Expected initialization message, got: ${messages.join(", ")}`,
    );
    assert.ok(homeStarted, "Home should have started");

    const { pathExists } =
      await import("../../src/domains/configuration/repositories/project-config.js");
    const configExists = await pathExists(path.join(tempDir, "conduit.yml"));
    assert.ok(configExists, "conduit.yml should exist after initialization");
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("bare conduit in uninitialized repo rejects and exits without writes", async () => {
  const tempDir = await setupGitRepo();
  try {
    const messages: string[] = [];
    const promptResponses = ["n"];
    const mockPrompt: PromptFn = async () => promptResponses.shift()!;
    let homeStarted = false;

    let exitCode: number | undefined;

    await handleBareConduit(tempDir, {
      prompt: mockPrompt,
      output: (msg) => messages.push(msg),
      setExitCode: (code) => {
        exitCode = code;
      },
      startHome: async () => {
        homeStarted = true;
      },
      startupMigration: async () => {},
      environment: { XDG_DATA_HOME: tempDir },
    });

    assert.equal(exitCode, 1);
    assert.ok(!homeStarted, "Home should not have started");

    const { pathExists } =
      await import("../../src/domains/configuration/repositories/project-config.js");
    const configExists = await pathExists(path.join(tempDir, "conduit.yml"));
    assert.ok(!configExists, "conduit.yml should not exist after rejection");
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("bare conduit in non-git directory prints error", async () => {
  const tempDir = await mkdtemp(path.join(tmpdir(), "conduit-test-"));
  try {
    const messages: string[] = [];
    const mockPrompt: PromptFn = async () => "y";
    let homeStarted = false;

    let exitCode: number | undefined;

    await handleBareConduit(tempDir, {
      prompt: mockPrompt,
      output: (msg) => messages.push(msg),
      setExitCode: (code) => {
        exitCode = code;
      },
      startHome: async () => {
        homeStarted = true;
      },
      startupMigration: async () => {},
      environment: { XDG_DATA_HOME: tempDir },
    });

    assert.equal(exitCode, 1);
    assert.ok(!homeStarted, "Home should not have started");
    assert.ok(
      messages.some((m) => m.includes("Not a Git repository")),
      `Expected Git error, got: ${messages.join(", ")}`,
    );
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("bare conduit in initialized repo enters Home directly", async () => {
  const tempDir = await setupGitRepo();
  try {
    const { initializeProject } =
      await import("../../src/domains/configuration/repositories/project-config.js");
    const { roleTemplates } =
      await import("../../src/domains/roles/assets/role-templates.js");
    const root = path.resolve(
      path.dirname(new URL(import.meta.url).pathname),
      "../..",
    );
    await initializeProject(tempDir, path.join(root, "skills"), roleTemplates);

    const messages: string[] = [];
    let homeStarted = false;

    await handleBareConduit(tempDir, {
      output: (msg) => messages.push(msg),
      startHome: async () => {
        homeStarted = true;
      },
      startupMigration: async () => {},
      environment: { XDG_DATA_HOME: tempDir },
    });

    assert.ok(homeStarted, "Home should have started");
    assert.ok(
      !messages.some((m) => m.includes("Initialize now")),
      "Should not prompt for initialization",
    );
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("bare conduit completes startup migration before opening Home", async () => {
  const tempDir = await setupGitRepo();
  try {
    const { initializeProject } =
      await import("../../src/domains/configuration/repositories/project-config.js");
    const { roleTemplates } =
      await import("../../src/domains/roles/assets/role-templates.js");
    const root = path.resolve(
      path.dirname(new URL(import.meta.url).pathname),
      "../..",
    );
    await initializeProject(tempDir, path.join(root, "skills"), roleTemplates);
    const order: string[] = [];
    await handleBareConduit(tempDir, {
      startupMigration: async () => {
        order.push("migration-start");
        await new Promise((resolve) => setTimeout(resolve, 5));
        order.push("migration-complete");
      },
      startHome: async () => {
        order.push("home-query");
      },
      environment: { XDG_DATA_HOME: tempDir },
    });
    assert.deepEqual(order, [
      "migration-start",
      "migration-complete",
      "home-query",
    ]);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("bare Home starts one non-blocking update request after migration", async () => {
  const tempDir = await setupGitRepo();
  try {
    const { initializeProject } =
      await import("../../src/domains/configuration/repositories/project-config.js");
    const { roleTemplates } =
      await import("../../src/domains/roles/assets/role-templates.js");
    const root = path.resolve(
      path.dirname(new URL(import.meta.url).pathname),
      "../..",
    );
    await initializeProject(tempDir, path.join(root, "skills"), roleTemplates);

    const order: string[] = [];
    let requestCount = 0;
    let releaseResponse: ((response: Response) => void) | undefined;
    const delayedResponse = new Promise<Response>((resolve) => {
      releaseResponse = resolve;
    });
    const discovery = new GitHubReleaseDiscovery({
      fetch: async () => {
        requestCount += 1;
        order.push("request-start");
        return delayedResponse;
      },
    });

    await handleBareConduit(tempDir, {
      startupMigration: async () => {
        order.push("migration-complete");
      },
      updatesBootstrapService: new UpdatesBootstrapService(discovery),
      startHome: async ({ queryBus, updateChecksEnabled }) => {
        order.push("home-render");
        assert.equal(updateChecksEnabled, true);
        assert.equal(requestCount, 1);
        const subscribers = Promise.all([
          queryBus.execute({ type: "checkForUpdate" }),
          queryBus.execute({ type: "checkForUpdate" }),
        ]);
        releaseResponse?.(
          new Response("[]", {
            status: 200,
            headers: { "content-type": "application/json" },
          }),
        );
        await subscribers;
      },
      environment: { XDG_DATA_HOME: tempDir },
    });

    assert.deepEqual(order.slice(0, 3), [
      "migration-complete",
      "request-start",
      "home-render",
    ]);
    assert.equal(requestCount, 1);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("non-interactive bare mode makes no update request or notice", async () => {
  const tempDir = await setupGitRepo();
  try {
    const { initializeProject } =
      await import("../../src/domains/configuration/repositories/project-config.js");
    const { roleTemplates } =
      await import("../../src/domains/roles/assets/role-templates.js");
    const root = path.resolve(
      path.dirname(new URL(import.meta.url).pathname),
      "../..",
    );
    await initializeProject(tempDir, path.join(root, "skills"), roleTemplates);
    let requestCount = 0;
    const messages: string[] = [];
    const discovery = new GitHubReleaseDiscovery({
      fetch: async () => {
        requestCount += 1;
        return new Response("[]");
      },
    });

    await handleBareConduit(tempDir, {
      checkForUpdates: false,
      output: (message) => messages.push(message),
      startupMigration: async () => {},
      updatesBootstrapService: new UpdatesBootstrapService(discovery),
      startHome: async ({ updateChecksEnabled }) => {
        assert.equal(updateChecksEnabled, false);
      },
      environment: { XDG_DATA_HOME: tempDir },
    });

    assert.equal(requestCount, 0);
    assert.equal(
      messages.some((message) => /update|version available/i.test(message)),
      false,
    );
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
