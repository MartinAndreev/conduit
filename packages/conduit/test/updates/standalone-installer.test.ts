import { test } from "bun:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  chmod,
  mkdtemp,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { InstallationKind } from "../../src/domains/updates/enums/installation-kind.js";
import {
  UpdateIntegrityError,
  UpdateReplacementError,
} from "../../src/domains/updates/errors/update-errors.js";
import { ReleaseAssetFetcher } from "../../src/domains/updates/repositories/release-asset-fetcher.js";
import { VerifiedStandaloneInstaller } from "../../src/domains/updates/repositories/verified-standalone-installer.js";
import type { StableRelease } from "../../src/domains/updates/types/release.js";
import type { ExecutableReplacer } from "../../src/domains/updates/interfaces/executable-replacer.js";

const assetName = "conduit-linux-x64";
const binaryUrl =
  "https://github.com/MartinAndreev/conduit/releases/download/v0.6.0/conduit-linux-x64";
const checksumUrl =
  "https://github.com/MartinAndreev/conduit/releases/download/v0.6.0/SHA256SUMS";

function fixtureRelease(binarySize: number): StableRelease {
  return {
    version: "0.6.0",
    tagName: "v0.6.0",
    publishedAt: "2026-07-15T08:00:00Z",
    releaseUrl: "https://github.com/MartinAndreev/conduit/releases/tag/v0.6.0",
    assets: [
      { name: assetName, url: binaryUrl, size: binarySize },
      { name: "SHA256SUMS", url: checksumUrl, size: 100 },
    ],
  };
}

test("standalone installer verifies and atomically replaces the executable", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "conduit-install-"));
  const executable = path.join(directory, "conduit");
  const binary = new TextEncoder().encode("new verified executable");
  const digest = createHash("sha256").update(binary).digest("hex");
  await writeFile(executable, "old executable");
  await chmod(executable, 0o755);
  const requested: string[] = [];
  const fetcher = new ReleaseAssetFetcher(async (input) => {
    const url = String(input);
    requested.push(url);
    return url === binaryUrl
      ? new Response(binary)
      : new Response(`${digest}  ${assetName}\n`);
  });
  const stages: string[] = [];
  try {
    await new VerifiedStandaloneInstaller(fetcher).install(
      {
        currentVersion: "0.5.4",
        release: fixtureRelease(binary.byteLength),
        installation: {
          kind: InstallationKind.Standalone,
          automatic: true,
          label: "Official standalone binary",
          executablePath: executable,
          assetName,
        },
      },
      (event) => stages.push(event.stage),
    );
    assert.deepEqual(await readFile(executable), Buffer.from(binary));
    assert.deepEqual(requested.sort(), [binaryUrl, checksumUrl].sort());
    assert.deepEqual(stages, [
      "preparing",
      "downloading",
      "verifying",
      "installing",
      "complete",
    ]);
    assert.deepEqual(
      (await readdir(directory)).filter((name) =>
        name.startsWith(".conduit-update-"),
      ),
      [],
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("checksum mismatch preserves the old executable and cleans staging", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "conduit-install-bad-"));
  const executable = path.join(directory, "conduit");
  const binary = new TextEncoder().encode("untrusted executable");
  await writeFile(executable, "known-good executable");
  await chmod(executable, 0o755);
  const fetcher = new ReleaseAssetFetcher(async (input) =>
    String(input) === binaryUrl
      ? new Response(binary)
      : new Response(`${"0".repeat(64)}  ${assetName}\n`),
  );
  try {
    await assert.rejects(
      new VerifiedStandaloneInstaller(fetcher).install(
        {
          currentVersion: "0.5.4",
          release: fixtureRelease(binary.byteLength),
          installation: {
            kind: InstallationKind.Standalone,
            automatic: true,
            label: "Official standalone binary",
            executablePath: executable,
            assetName,
          },
        },
        () => undefined,
      ),
      (error: unknown) =>
        error instanceof UpdateIntegrityError &&
        error.code === "CHECKSUM_MISMATCH",
    );
    assert.equal(await readFile(executable, "utf8"), "known-good executable");
    assert.deepEqual(
      (await readdir(directory)).filter((name) =>
        name.startsWith(".conduit-update-"),
      ),
      [],
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("checksum parser rejects missing, duplicate, and malformed entries", async () => {
  const { expectedSha256 } =
    await import("../../src/domains/updates/helpers/checksum.js");
  assert.throws(() => expectedSha256(`${"a".repeat(64)}  other\n`, assetName));
  assert.throws(() =>
    expectedSha256(
      `${"a".repeat(64)}  ${assetName}\n${"b".repeat(64)}  ${assetName}\n`,
      assetName,
    ),
  );
  assert.throws(() =>
    expectedSha256(`not-a-checksum  ${assetName}\n`, assetName),
  );
});

test("replacement failure retains the verified old executable", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "conduit-replace-bad-"));
  const executable = path.join(directory, "conduit");
  const binary = new TextEncoder().encode("new executable");
  const digest = createHash("sha256").update(binary).digest("hex");
  await writeFile(executable, "old executable");
  await chmod(executable, 0o755);
  const fetcher = new ReleaseAssetFetcher(async (input) =>
    String(input) === binaryUrl
      ? new Response(binary)
      : new Response(`${digest}  ${assetName}\n`),
  );
  const replacer: ExecutableReplacer = {
    replace: async () => {
      throw new Error("simulated locked destination");
    },
  };
  try {
    await assert.rejects(
      new VerifiedStandaloneInstaller(fetcher, replacer).install(
        {
          currentVersion: "0.5.4",
          release: fixtureRelease(binary.byteLength),
          installation: {
            kind: InstallationKind.Standalone,
            automatic: true,
            label: "Official standalone binary",
            executablePath: executable,
            assetName,
          },
        },
        () => undefined,
      ),
      (error: unknown) => error instanceof UpdateReplacementError,
    );
    assert.equal(await readFile(executable, "utf8"), "old executable");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("asset fetch rejects redirects outside official release hosts", async () => {
  const fetcher = new ReleaseAssetFetcher(
    async () =>
      new Response(null, {
        status: 302,
        headers: { location: "https://example.com/untrusted-binary" },
      }),
  );
  await assert.rejects(fetcher.fetch(binaryUrl, 1024), /not approved/i);
});

test("standalone installer rejects assets from a different release tag", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "conduit-wrong-tag-"));
  const executable = path.join(directory, "conduit");
  await writeFile(executable, "old executable");
  await chmod(executable, 0o755);
  const release = fixtureRelease(10);
  const wrongTagRelease: StableRelease = {
    ...release,
    assets: release.assets.map((asset) => ({
      ...asset,
      url: asset.url.replace("/v0.6.0/", "/v0.5.4/"),
    })),
  };
  try {
    await assert.rejects(
      new VerifiedStandaloneInstaller(
        new ReleaseAssetFetcher(async () => new Response("unused")),
      ).install(
        {
          currentVersion: "0.5.4",
          release: wrongTagRelease,
          installation: {
            kind: InstallationKind.Standalone,
            automatic: true,
            label: "Official standalone binary",
            executablePath: executable,
            assetName,
          },
        },
        () => undefined,
      ),
      /does not belong to the official release/i,
    );
    assert.equal(await readFile(executable, "utf8"), "old executable");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
