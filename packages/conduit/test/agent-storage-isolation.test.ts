import { test } from "bun:test";
import { execFileSync } from "node:child_process";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  agentProcessEnvironment,
  planRun,
} from "../src/domains/runs/repositories/run-orchestrator.js";
import type { Config } from "../src/domains/configuration/types/config.js";

test("spawned agents receive neither database context nor rejection for overlapping ownership", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-agent-isolation-"));
  const config: Config = {
    version: 1,
    specsDir: "specs",
    stateDir: ".conduit",
    roles: {
      backend: {
        runner: "codex",
        mode: "write",
        owns: ["packages/conduit/src"],
        skill: { source: "missing-local-skill.md" },
      },
      frontend: {
        runner: "codex",
        mode: "write",
        owns: ["packages/conduit/src"],
        skill: { source: "missing-local-skill.md" },
      },
    },
  };
  try {
    execFileSync("git", ["-C", projectRoot, "init"]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "config",
      "user.email",
      "test@example.com",
    ]);
    execFileSync("git", ["-C", projectRoot, "config", "user.name", "Test"]);
    const packetDirectory = join(projectRoot, "specs", "002-demo");
    await mkdir(join(packetDirectory, "contracts"), { recursive: true });
    await writeFile(
      join(packetDirectory, "spec.md"),
      "# Demo\n\nAcceptance criterion from the live packet.\n",
    );
    await writeFile(
      join(packetDirectory, "contracts", "README.md"),
      "# Contract\n\nReturn the stable response shape.\n",
    );
    execFileSync("git", ["-C", projectRoot, "add", "."]);
    execFileSync("git", [
      "-C",
      projectRoot,
      "-c",
      "commit.gpgSign=false",
      "commit",
      "-m",
      "packet",
    ]);
    await writeFile(join(projectRoot, "LOCAL_CHANGE.txt"), "dirty\n");
    const sharedResearch = await planRun({
      projectRoot,
      config,
      featureId: "002",
      roleNames: ["backend"],
      builtinRoot: join(projectRoot, "missing-builtins"),
      sharedReadOnlyWorkspace: true,
    });
    assert.equal(sharedResearch.run.roles[0]?.readOnly, true);
    assert.equal(sharedResearch.run.roles[0]?.worktree, projectRoot);
    assert.equal(sharedResearch.run.roles[0]?.workspaceRepositoryId, undefined);
    await assert.rejects(
      () =>
        planRun({
          projectRoot,
          config,
          featureId: "002",
          roleNames: ["backend", "frontend"],
          builtinRoot: join(projectRoot, "missing-builtins"),
        }),
      /require a clean project worktree/,
    );
    await rm(join(projectRoot, "LOCAL_CHANGE.txt"));

    const environment = agentProcessEnvironment({
      PATH: "/usr/bin",
      TURSO_AUTH_TOKEN: "database-secret",
      LIBSQL_URL: "file:///private/state.db",
      DATABASE_URL: "libsql://private.example",
      CONDUIT_DB_PATH: "/private/state.db",
      OPENAI_API_KEY: "runner-credential",
    });
    assert.equal(environment.PATH, "/usr/bin");
    assert.equal(environment.OPENAI_API_KEY, "runner-credential");
    assert.equal(environment.TURSO_AUTH_TOKEN, undefined);
    assert.equal(environment.LIBSQL_URL, undefined);
    assert.equal(environment.DATABASE_URL, undefined);
    assert.equal(environment.CONDUIT_DB_PATH, undefined);

    const { run } = await planRun({
      projectRoot,
      config,
      featureId: "002",
      roleNames: ["backend", "frontend"],
      builtinRoot: join(projectRoot, "missing-builtins"),
    });
    assert.equal(run.roles.length, 2);
    const role = run.roles[0];
    assert.ok(role);
    assert.deepEqual(role.assignment?.forbiddenPaths, [".git", ".conduit"]);
    const suppliedContext = [
      role.prompt,
      await readFile(role.promptFile, "utf8"),
      role.command,
      ...role.args,
    ].join("\n");
    assert.doesNotMatch(
      suppliedContext,
      /state\.db|global\.db|libsql:|turso_auth|database_url|kysely/i,
    );
    assert.match(
      role.context ?? "",
      /Acceptance criterion from the live packet/,
    );
    assert.match(role.context ?? "", /Return the stable response shape/);
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
