import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createAgentResponseToolRuntime } from "../../src/system/communication/services/agent-response-tool-runtime.js";

const validResponse = {
  protocolVersion: "1.0",
  status: "completed",
  summary: "accepted",
  verdict: "none",
  verdictRationale: "",
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

test("standalone response tool runtime invokes its bundled executable directly", () => {
  const priorEntry = process.argv[1];
  process.argv[1] = "/$bunfs/root/conduit.js";
  const runtime = createAgentResponseToolRuntime();
  try {
    assert.deepEqual(runtime.mcpServer.args, [
      ...process.execArgv,
      "__agent-response-mcp",
    ]);
  } finally {
    runtime.cleanup();
    process.argv[1] = priorEntry;
  }
});

test("response tool MCP environment strips credentials and database access", () => {
  const priorApiKey = process.env.OPENAI_API_KEY;
  const priorDatabase = process.env.DATABASE_URL;
  process.env.OPENAI_API_KEY = "sentinel-api-key";
  process.env.DATABASE_URL = "sentinel-database";
  const runtime = createAgentResponseToolRuntime();
  try {
    const environment = Object.fromEntries(
      runtime.mcpServer.env.map((entry) => [entry.name, entry.value]),
    );
    assert.equal(environment.OPENAI_API_KEY, "");
    assert.equal(environment.DATABASE_URL, "");
    assert.ok(environment.CONDUIT_AGENT_RESPONSE_CAPTURE);
    assert.doesNotMatch(JSON.stringify(runtime.mcpServer), /sentinel/);
  } finally {
    runtime.cleanup();
    if (priorApiKey === undefined) delete process.env.OPENAI_API_KEY;
    else process.env.OPENAI_API_KEY = priorApiKey;
    if (priorDatabase === undefined) delete process.env.DATABASE_URL;
    else process.env.DATABASE_URL = priorDatabase;
  }
});

test("agent response MCP tool rejects malformed input and captures a corrected call", async () => {
  const directory = await mkdtemp(
    path.join(os.tmpdir(), "conduit-response-mcp-test-"),
  );
  const capturePath = path.join(directory, "response.json");
  try {
    const missingFindings: Record<string, unknown> = { ...validResponse };
    delete missingFindings.findings;
    const requests = [
      {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: "2024-11-05" },
      },
      { jsonrpc: "2.0", id: 2, method: "tools/list", params: {} },
      {
        jsonrpc: "2.0",
        id: 3,
        method: "tools/call",
        params: {
          name: "submit_agent_response",
          arguments: missingFindings,
        },
      },
      {
        jsonrpc: "2.0",
        id: 4,
        method: "tools/call",
        params: {
          name: "submit_agent_response",
          arguments: { ...validResponse, findings: "invalid" },
        },
      },
      {
        jsonrpc: "2.0",
        id: 5,
        method: "tools/call",
        params: {
          name: "submit_agent_response",
          arguments: {
            ...validResponse,
            findings: [
              {
                severity: "error",
                category: "protocol",
                message: "invalid evidence",
                evidence: 42,
              },
            ],
          },
        },
      },
      {
        jsonrpc: "2.0",
        id: 6,
        method: "tools/call",
        params: { name: "submit_agent_response", arguments: validResponse },
      },
    ];
    const output = execFileSync(
      process.execPath,
      ["bin/conduit.js", "__agent-response-mcp"],
      {
        cwd: path.resolve(import.meta.dirname, "../.."),
        env: {
          ...process.env,
          CONDUIT_AGENT_RESPONSE_CAPTURE: capturePath,
        },
        input: `${[
          ...requests.slice(0, 2).map((request) => JSON.stringify(request)),
          JSON.stringify({
            jsonrpc: "2.0",
            id: 99,
            method: "tools/call",
            params: { padding: "x".repeat(300_000) },
          }),
          ...requests.slice(2).map((request) => JSON.stringify(request)),
        ].join("\n")}\n`,
        encoding: "utf8",
      },
    );
    const responses = output
      .trim()
      .split("\n")
      .map((line) => JSON.parse(line));
    assert.equal(responses[1]?.result.tools[0].name, "submit_agent_response");
    assert.ok(
      responses[1]?.result.tools[0].inputSchema.properties.findings.items.required.includes(
        "evidence",
      ),
    );
    assert.ok(
      responses[1]?.result.tools[0].inputSchema.properties.globalPromotionProposals.items.required.includes(
        "evidence",
      ),
    );
    assert.equal(
      responses[1]?.result.tools[0].inputSchema.properties.findings.items
        .properties.evidence.type,
      "string",
    );
    assert.match(responses[2]?.error.message, /size limit/);
    assert.equal(responses[3]?.result.isError, true);
    assert.match(responses[3]?.result.content[0].text, /findings/);
    assert.equal(responses[4]?.result.isError, true);
    assert.match(responses[4]?.result.content[0].text, /findings/);
    assert.equal(responses[5]?.result.isError, true);
    assert.match(
      responses[5]?.result.content[0].text,
      /findings\[0\]\.evidence/,
    );
    assert.ok(responses[5]?.result.content[0].text.length <= 2_000);
    assert.equal(responses[6]?.result.isError, undefined);
    const canonicalResponse: Record<string, unknown> = { ...validResponse };
    delete canonicalResponse.verdictRationale;
    canonicalResponse.verdict = null;
    assert.deepEqual(
      JSON.parse(await readFile(capturePath, "utf8")),
      canonicalResponse,
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
