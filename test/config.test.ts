import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultConfig,
  parseConfig,
  serializeConfig,
} from "../src/domains/configuration/repositories/project-config.js";

test("generated config round-trips built-in roles", () => {
  const parsed = parseConfig(serializeConfig(defaultConfig));
  assert.equal(parsed.roles.frontend.runner, "opencode");
  assert.deepEqual(parsed.roles.frontend.owns, ["apps/web", "packages/ui"]);
  assert.equal(
    parsed.roles.architect.skill.source as string,
    "file:.conduit/roles/architect.md",
  );
  assert.deepEqual(parsed.roles.documentation.owns, ["docs", "README.md"]);
  assert.match(parsed.roles.documentation.description!, /documentation/);
});

test("config preserves a role model override", () => {
  const parsed = parseConfig(
    "version: 1\nroles:\n  frontend:\n    runner: opencode\n    mode: subagent\n    model: openai/gpt-5-mini\n    skill:\n      source: builtin:frontend\n",
  );
  assert.equal(parsed.roles.frontend.model, "openai/gpt-5-mini");
});

test("config preserves a role reasoning-effort preference", () => {
  const parsed = parseConfig(
    "version: 1\nroles:\n  frontend:\n    runner: opencode\n    mode: subagent\n    effort: high\n    skill:\n      source: builtin:frontend\n",
  );
  assert.equal(parsed.roles.frontend.effort, "high");
  assert.match(serializeConfig(parsed), /effort: high/);
});
