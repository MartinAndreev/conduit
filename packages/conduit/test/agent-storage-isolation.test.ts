import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  agentProcessEnvironment,
  planRun,
} from "../src/domains/runs/repositories/run-orchestrator.js";
import type { Config } from "../src/domains/configuration/types/config.js";

test("spawned agents receive neither database environment nor database context", async () => {
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
    },
  };
  try {
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
      roleNames: ["backend"],
      builtinRoot: join(projectRoot, "missing-builtins"),
    });
    const role = run.roles[0];
    assert.ok(role);
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
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
