import { afterEach, beforeEach, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import {
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

let root: string;
let home: string;

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "conduit-install-"));
  writeFileSync(join(root, "package.json"), '{"type":"module"}');
  home = join(root, "user's home");
  mkdirSync(home);
  mkdirSync(join(root, "scripts"));
  mkdirSync(join(root, "dist", "release"), { recursive: true });
  copyFileSync(
    new URL("../scripts/install-local.ts", import.meta.url),
    join(root, "scripts", "install-local.ts"),
  );
  for (const target of [
    "linux-x64",
    "linux-arm64",
    "darwin-arm64",
    "windows-x64",
  ]) {
    writeFileSync(
      join(
        root,
        "dist",
        "release",
        `conduit-${target}${target === "windows-x64" ? ".exe" : ""}`,
      ),
      target,
    );
  }
});

afterEach(() => rmSync(root, { recursive: true, force: true }));

function install(
  args: readonly string[] = ["darwin-arm64"],
  shell = "zsh",
  path = "/usr/bin:/bin",
) {
  return spawnSync(
    "node",
    [
      "--import",
      "tsx",
      "--input-type=module",
      "--eval",
      `
    import os from 'node:os';
    import { syncBuiltinESMExports } from 'node:module';
    os.homedir = () => ${JSON.stringify(home)};
    syncBuiltinESMExports();
    process.env.SHELL = ${JSON.stringify(`/bin/${shell}`)};
    process.env.ZDOTDIR = ${JSON.stringify(home)};
    process.env.PATH = ${JSON.stringify(path)};
    process.argv = ['node', 'installer', ...${JSON.stringify(args)}];
    await import(${JSON.stringify(pathToFileURL(join(root, "scripts", "install-local.ts")).href)});
  `,
    ],
    { encoding: "utf8" },
  );
}

test("installs all targets with executable permissions and accepts --", () => {
  for (const target of [
    "linux-x64",
    "linux-arm64",
    "darwin-arm64",
    "windows-x64",
  ]) {
    const result = install(["--", target]);
    expect(result.status).toBe(0);
    const destination = join(
      home,
      ".local/bin",
      `conduit${target === "windows-x64" ? ".exe" : ""}`,
    );
    expect(readFileSync(destination, "utf8")).toBe(target);
    expect(statSync(destination).mode & 0o777).toBe(0o755);
  }
});

test("configures zsh without replacing existing content or duplicating the block", () => {
  const config = join(home, ".zshrc");
  writeFileSync(config, "# existing shell configuration\n");
  expect(install().status).toBe(0);
  const first = readFileSync(config, "utf8");
  const again = install();
  expect(again.status).toBe(0);
  expect(readFileSync(config, "utf8")).toBe(first);
  expect(first).toStartWith("# existing shell configuration\n");
  expect(again.stdout).toContain("current terminal");
  const shell = spawnSync(
    "/bin/sh",
    [
      "-c",
      'PATH=/usr/bin:/bin; . "$1"; . "$1"; command -v conduit; printf "%s" "$PATH"',
      "sh",
      config,
    ],
    { encoding: "utf8" },
  );
  expect(shell.status).toBe(0);
  expect(shell.stdout).toBe(
    `${join(home, ".local/bin/conduit")}\n${join(home, ".local/bin")}:/usr/bin:/bin`,
  );
});

test("leaves startup files alone when the directory is already on PATH", () => {
  expect(
    install(["darwin-arm64"], "zsh", `${join(home, ".local/bin")}:/usr/bin`)
      .status,
  ).toBe(0);
  expect(existsSync(join(home, ".zshrc"))).toBe(false);
});

test("bash preserves the existing login-file selection", () => {
  writeFileSync(join(home, ".profile"), "# existing profile\n");
  expect(install(["darwin-arm64"], "bash").status).toBe(0);
  expect(readFileSync(join(home, ".profile"), "utf8")).toStartWith(
    "# existing profile\n",
  );
  expect(readFileSync(join(home, ".bashrc"), "utf8")).toContain(
    "Conduit local binary",
  );
  expect(existsSync(join(home, ".bash_profile"))).toBe(false);
});

test("prints actionable guidance if shell configuration cannot be written", () => {
  mkdirSync(join(home, ".zshrc"));
  const result = install();
  expect(result.status).toBe(0);
  expect(result.stderr).toContain("Could not configure PATH");
  expect(result.stdout).toContain("export PATH=");
  expect(existsSync(join(home, ".local/bin/conduit"))).toBe(true);
});

test("rejects invalid targets and missing artifacts before changing shell files", () => {
  expect(install(["../invalid"]).status).toBe(1);
  expect(install(["darwin-arm64", "extra"]).status).toBe(1);
  rmSync(join(root, "dist/release/conduit-darwin-arm64"));
  const result = install();
  expect(result.status).toBe(1);
  expect(result.stderr).toContain("build:standalone darwin-arm64");
  expect(existsSync(join(home, ".zshrc"))).toBe(false);
});

test("defaults to the host target and provides help without installation", () => {
  expect(install(["--help"]).status).toBe(0);
  expect(existsSync(join(home, ".local"))).toBe(false);
  const hostTarget = `${process.platform === "win32" ? "windows" : process.platform}-${process.arch}`;
  const result = install([]);
  expect(result.status).toBe(
    ["linux-x64", "linux-arm64", "darwin-arm64", "windows-x64"].includes(
      hostTarget,
    )
      ? 0
      : 1,
  );
  expect(result.stdout + result.stderr).toContain(hostTarget);
});
