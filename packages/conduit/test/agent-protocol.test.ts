import assert from "node:assert/strict";
import test from "node:test";
import { parseAgentResponseV1 } from "../src/domains/runs/validation/agent-response-validator.js";
import { validateAgentResponseForAssignment } from "../src/domains/runs/validation/agent-semantic-validator.js";
import { agentProcessEnvironment } from "../src/domains/runs/repositories/run-orchestrator.js";
import type { AgentResponseV1 } from "../src/domains/runs/types/agent-protocol.js";

const base: AgentResponseV1 = {
  protocolVersion: "1.0",
  status: "completed",
  summary: "Completed work.",
  verdict: null,
  artifacts: [],
  findings: [],
  verification: [],
  decisions: [],
  blockers: [],
  questions: [],
  risks: [],
  evidence: [],
  memoryProposals: [],
  globalPromotionProposals: [],
};

test("AgentResponseV1 rejects prose, unknown properties, invalid paths, and secrets", () => {
  assert.equal(parseAgentResponseV1("```json\n{}\n```").valid, false);
  assert.equal(parseAgentResponseV1(JSON.stringify({ ...base, extra: true })).valid, false);
  assert.equal(parseAgentResponseV1(JSON.stringify({ ...base, artifacts: [{ path: "../x", category: "source", purpose: "x", action: "modified" }] })).valid, false);
  assert.equal(parseAgentResponseV1(JSON.stringify({ ...base, summary: "api_key=secret-value" })).valid, false);
});

test("semantic policy differs by assignment role", () => {
  const parsed = parseAgentResponseV1(JSON.stringify({ ...base, findings: [{ severity: "info", category: "fact", message: "Repo uses CQRS", evidence: ["packages/conduit/src/system/bus"] }] }));
  assert.equal(parsed.valid, true);
  assert.equal(validateAgentResponseForAssignment(parsed.value!, { roleKind: "research", ownedPaths: [] }).valid, true);
  assert.equal(validateAgentResponseForAssignment(parsed.value!, { roleKind: "reviewer", ownedPaths: [] }).valid, false);
});

test("implementation completion requires owned artifacts and verification", () => {
  const response = parseAgentResponseV1(JSON.stringify({ ...base, artifacts: [{ path: "src/a.ts", category: "source", purpose: "change", action: "modified" }], verification: [{ operation: "bun test", outcome: "passed", summary: "ok" }] })).value!;
  assert.equal(validateAgentResponseForAssignment(response, { roleKind: "implementation", ownedPaths: ["src"] }).valid, true);
  assert.equal(validateAgentResponseForAssignment(response, { roleKind: "implementation", ownedPaths: ["docs"] }).valid, false);
});

test("blocked and needs_input require structured sections", () => {
  assert.equal(validateAgentResponseForAssignment({ ...base, status: "blocked" }, { roleKind: "custom", ownedPaths: [] }).valid, false);
  assert.equal(validateAgentResponseForAssignment({ ...base, status: "needs_input" }, { roleKind: "custom", ownedPaths: [] }).valid, false);
});

test("agent process environment removes database configuration", () => {
  const env = agentProcessEnvironment({ TURSO_DATABASE_URL: "x", LIBSQL_AUTH_TOKEN: "y", CONDUIT_DB_PATH: "z", SAFE: "1" });
  assert.deepEqual(env, { SAFE: "1" });
});
