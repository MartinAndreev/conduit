import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { InstallationKind } from "../../src/domains/updates/enums/installation-kind.js";
import { DefaultInstallationDetector } from "../../src/domains/updates/repositories/default-installation-detector.js";

test("standalone detection enables only writable tested Unix targets", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "conduit-detect-"));
  try {
    const linux = await new DefaultInstallationDetector({
      standaloneBuild: true,
      executablePath: path.join(directory, "conduit"),
      platform: "linux",
      architecture: "x64",
    }).detect();
    assert.equal(linux.kind, InstallationKind.Standalone);
    assert.equal(linux.automatic, true);
    assert.equal(linux.assetName, "conduit-linux-x64");

    const windows = await new DefaultInstallationDetector({
      standaloneBuild: true,
      executablePath: path.join(directory, "conduit.exe"),
      platform: "win32",
      architecture: "x64",
    }).detect();
    assert.equal(windows.kind, InstallationKind.Unsupported);
    assert.equal(windows.automatic, false);
    assert.match(windows.reason ?? "", /deferred replacement/i);

    const unsupported = await new DefaultInstallationDetector({
      standaloneBuild: true,
      executablePath: path.join(directory, "conduit"),
      platform: "darwin",
      architecture: "x64",
    }).detect();
    assert.equal(unsupported.kind, InstallationKind.Unsupported);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("package detection requires positive global-manager path evidence", async () => {
  const cases = [
    ["/usr/local/lib/node_modules/conduit-orchestrator/dist/conduit.js", "npm"],
    [
      "/home/me/.local/share/pnpm/global/5/.pnpm/conduit-orchestrator@0.6.0/node_modules/conduit-orchestrator/dist/conduit.js",
      "pnpm",
    ],
    [
      "/home/me/.bun/install/global/node_modules/conduit-orchestrator/dist/conduit.js",
      "bun",
    ],
  ] as const;
  for (const [entryPath, manager] of cases) {
    const result = await new DefaultInstallationDetector({
      standaloneBuild: false,
      executablePath: process.execPath,
      entryPath,
      platform: "linux",
      architecture: "x64",
    }).detect();
    assert.equal(result.kind, InstallationKind.GlobalPackage);
    assert.equal(result.packageManager, manager);
  }
});

test("local, source, and ambiguous installs remain non-mutating", async () => {
  const local = await new DefaultInstallationDetector({
    standaloneBuild: false,
    executablePath: process.execPath,
    entryPath: "/workspace/node_modules/conduit-orchestrator/dist/conduit.js",
    platform: "linux",
    architecture: "x64",
  }).detect();
  assert.equal(local.kind, InstallationKind.LocalDependency);
  assert.equal(local.automatic, false);

  const source = await new DefaultInstallationDetector({
    standaloneBuild: false,
    executablePath: process.execPath,
    entryPath: "/workspace/packages/conduit/bin/conduit.js",
    platform: "linux",
    architecture: "x64",
  }).detect();
  assert.equal(source.kind, InstallationKind.SourceCheckout);

  const unknown = await new DefaultInstallationDetector({
    standaloneBuild: false,
    executablePath: process.execPath,
    entryPath: "/opt/custom/conduit.js",
    platform: "linux",
    architecture: "x64",
  }).detect();
  assert.equal(unknown.kind, InstallationKind.Unknown);
  assert.equal(unknown.automatic, false);
  assert.match(unknown.manualUrl ?? "", /^https:/);
});
