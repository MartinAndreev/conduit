import { test } from "bun:test";
import assert from "node:assert/strict";
import { InstallationKind } from "../../src/domains/updates/enums/installation-kind.js";
import { UpdateProcessError } from "../../src/domains/updates/errors/update-errors.js";
import type { ProcessExecutor } from "../../src/domains/updates/interfaces/process-executor.js";
import { PackageUpdateInstaller } from "../../src/domains/updates/repositories/package-update-installer.js";
import type { ProcessExecutionRequest } from "../../src/domains/updates/types/process-execution.js";
import { NodeProcessExecutor } from "../../src/domains/updates/repositories/node-process-executor.js";
import { access, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

const release = {
  version: "0.6.0",
  tagName: "v0.6.0",
  publishedAt: "2026-07-15T08:00:00Z",
  releaseUrl: "https://github.com/MartinAndreev/conduit/releases/tag/v0.6.0",
  assets: [],
};

test("global package update uses a fixed executable and exact-version arguments", async () => {
  let observed: ProcessExecutionRequest | undefined;
  const executor: ProcessExecutor = {
    execute: async (request) => {
      observed = request;
      return { exitCode: 0, stdout: "ok", stderr: "", timedOut: false };
    },
  };
  const stages: string[] = [];
  await new PackageUpdateInstaller(executor).install(
    {
      currentVersion: "0.5.4",
      release,
      installation: {
        kind: InstallationKind.GlobalPackage,
        automatic: true,
        label: "pnpm global package",
        packageManager: "pnpm",
      },
    },
    (event) => stages.push(event.stage),
  );
  assert.equal(observed?.executable, "pnpm");
  assert.deepEqual(observed?.arguments, [
    "add",
    "--global",
    "conduit-orchestrator@0.6.0",
  ]);
  assert.ok(observed?.cwd.includes("conduit-update-"));
  await assert.rejects(access(observed!.cwd));
  assert.deepEqual(stages, ["preparing", "installing", "complete"]);
});

test("npm and bun strategies use their fixed global argument forms", async () => {
  const observed: ProcessExecutionRequest[] = [];
  const executor: ProcessExecutor = {
    execute: async (request) => {
      observed.push(request);
      return { exitCode: 0, stdout: "", stderr: "", timedOut: false };
    },
  };
  for (const manager of ["npm", "bun"] as const)
    await new PackageUpdateInstaller(executor).install(
      {
        currentVersion: "0.5.4",
        release,
        installation: {
          kind: InstallationKind.GlobalPackage,
          automatic: true,
          label: `${manager} global package`,
          packageManager: manager,
        },
      },
      () => undefined,
    );
  assert.deepEqual(
    observed.map(({ executable, arguments: args }) => [executable, ...args]),
    [
      ["npm", "install", "--global", "conduit-orchestrator@0.6.0"],
      ["bun", "add", "--global", "conduit-orchestrator@0.6.0"],
    ],
  );
});

test("package failure exposes only bounded sanitized diagnostics", async () => {
  const executor: ProcessExecutor = {
    execute: async () => ({
      exitCode: 1,
      stdout: "",
      stderr: `authorization: Bearer secret-value\nregistry unavailable`,
      timedOut: false,
    }),
  };
  await assert.rejects(
    new PackageUpdateInstaller(executor).install(
      {
        currentVersion: "0.5.4",
        release,
        installation: {
          kind: InstallationKind.GlobalPackage,
          automatic: true,
          label: "npm global package",
          packageManager: "npm",
        },
      },
      () => undefined,
    ),
    (error: unknown) =>
      error instanceof UpdateProcessError &&
      error.message.includes("registry unavailable") &&
      !error.message.includes("secret-value"),
  );
});

test("package process primitive strips unrelated environment secrets", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "conduit-process-env-"));
  const previous = process.env.CONDUIT_PROVIDER_TOKEN;
  process.env.CONDUIT_PROVIDER_TOKEN = "must-not-leak";
  try {
    const result = await new NodeProcessExecutor().execute({
      executable: process.execPath,
      arguments: [
        "-e",
        "process.stdout.write(process.env.CONDUIT_PROVIDER_TOKEN ?? 'absent')",
      ],
      cwd: directory,
      timeoutMs: 5_000,
      maximumOutputBytes: 1_024,
    });
    assert.equal(result.exitCode, 0);
    assert.equal(result.stdout, "absent");
  } finally {
    if (previous === undefined) delete process.env.CONDUIT_PROVIDER_TOKEN;
    else process.env.CONDUIT_PROVIDER_TOKEN = previous;
    await rm(directory, { recursive: true, force: true });
  }
});

test("global package update leaves project manifests and lockfiles unchanged", async () => {
  const project = await mkdtemp(path.join(tmpdir(), "conduit-project-"));
  const manifest = path.join(project, "package.json");
  const lockfile = path.join(project, "pnpm-lock.yaml");
  await writeFile(manifest, '{"name":"consumer","private":true}\n');
  await writeFile(lockfile, "lockfileVersion: '9.0'\n");
  const beforeManifest = await readFile(manifest, "utf8");
  const beforeLockfile = await readFile(lockfile, "utf8");
  const executor: ProcessExecutor = {
    execute: async (request) => {
      assert.notEqual(request.cwd, project);
      assert.match(request.cwd, /conduit-update-/);
      return { exitCode: 0, stdout: "", stderr: "", timedOut: false };
    },
  };
  try {
    await new PackageUpdateInstaller(executor).install(
      {
        currentVersion: "0.5.4",
        release,
        installation: {
          kind: InstallationKind.GlobalPackage,
          automatic: true,
          label: "pnpm global package",
          packageManager: "pnpm",
        },
      },
      () => undefined,
    );
    assert.equal(await readFile(manifest, "utf8"), beforeManifest);
    assert.equal(await readFile(lockfile, "utf8"), beforeLockfile);
  } finally {
    await rm(project, { recursive: true, force: true });
  }
});
