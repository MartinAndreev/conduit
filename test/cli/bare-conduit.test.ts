import { test } from "node:test";
import assert from "node:assert/strict";
import { tmpdir } from "node:os";
import path from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { handleBareConduit } from "../../src/cli.js";
import type { PromptFn } from "../../src/helpers/prompt.js";

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
    });

    assert.ok(
      messages.some((m) => m.includes("Conduit initialized")),
      `Expected initialization message, got: ${messages.join(", ")}`,
    );
    assert.ok(homeStarted, "Home should have started");

    const { pathExists } = await import("../../src/config.js");
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
    });

    assert.equal(exitCode, 1);
    assert.ok(!homeStarted, "Home should not have started");

    const { pathExists } = await import("../../src/config.js");
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
    const { initializeProject } = await import("../../src/config.js");
    const { roleTemplates } = await import("../../src/role-templates.js");
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
