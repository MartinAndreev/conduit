import { test } from "bun:test";
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { chmod, copyFile, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const hosts = {
  "linux:x64": {
    target: "linux-x64",
    artifact: "conduit-linux-x64",
    binding: "@tursodatabase/database-linux-x64-gnu",
  },
  "linux:arm64": {
    target: "linux-arm64",
    artifact: "conduit-linux-arm64",
    binding: "@tursodatabase/database-linux-arm64-gnu",
  },
  "darwin:arm64": {
    target: "darwin-arm64",
    artifact: "conduit-darwin-arm64",
    binding: "@tursodatabase/database-darwin-arm64",
  },
  "win32:x64": {
    target: "windows-x64",
    artifact: "conduit-windows-x64.exe",
    binding: "@tursodatabase/database-win32-x64-msvc",
  },
} as const;

const hostKey = `${process.platform}:${process.arch}` as keyof typeof hosts;
const host = hosts[hostKey];

(host ? test : test.skip)(
  "standalone executable embeds Turso and verifies persistence without node_modules",
  async () => {
    const directory = await mkdtemp(join(tmpdir(), "conduit-standalone-"));
    const projectRoot = join(directory, "project");
    const globalRoot = join(directory, "global");
    const executable = join(
      directory,
      process.platform === "win32" ? "conduit.exe" : "conduit",
    );
    const built = resolve("dist/release", host!.artifact);
    const seededSecret = "standalone-environment-secret-24680";
    try {
      execFileSync("bun", ["scripts/build-standalone.js", host!.target], {
        stdio: "pipe",
      });
      await copyFile(built, executable);
      await chmod(executable, 0o755);
      execFileSync("git", ["init", "-q", projectRoot]);
      execFileSync(executable, ["init", projectRoot], { stdio: "pipe" });
      const output = execFileSync(
        executable,
        ["storage-doctor", "--project", projectRoot],
        {
          cwd: directory,
          encoding: "utf8",
          env: {
            ...process.env,
            XDG_DATA_HOME: globalRoot,
            APPDATA: globalRoot,
            CONDUIT_TEST_TOKEN: seededSecret,
          },
        },
      );
      const diagnostic = JSON.parse(output) as {
        binding: string;
        projectDatabase: string;
        globalDatabase: string;
        projectMigrationCount: number;
        globalMigrationCount: number;
        interruptedMigrationRecovered: boolean;
      };
      assert.equal(diagnostic.binding, host!.binding);
      assert.equal(diagnostic.projectMigrationCount, 4);
      assert.equal(diagnostic.globalMigrationCount, 1);
      assert.equal(diagnostic.interruptedMigrationRecovered, true);
      for (const databasePath of [
        diagnostic.projectDatabase,
        diagnostic.globalDatabase,
      ])
        assert.equal(
          (await readFile(databasePath)).includes(seededSecret),
          false,
        );
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  },
  30_000,
);

test("standalone build rejects unsupported Intel macOS and musl targets", () => {
  for (const target of ["darwin-x64", "linux-x64-musl"]) {
    const result = spawnSync("bun", ["scripts/build-standalone.js", target], {
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(`${result.stdout}${result.stderr}`, /unsupported/);
  }
});
