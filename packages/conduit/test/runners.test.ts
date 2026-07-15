import { test } from "bun:test";
import assert from "node:assert/strict";
import { CodexAdapter } from "../src/system/runners/codex.js";
import { OpenCodeAdapter } from "../src/system/runners/opencode.js";
import { PiAdapter } from "../src/system/runners/pi.js";
import { KiloAdapter } from "../src/system/runners/kilo.js";
import { createUnavailableEvent } from "../src/system/runners/unavailable.js";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { LifecyclePayload } from "../src/domains/runs/types/runner-events.js";
import { captureFinalResponse } from "../src/system/runners/registry.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test("CodexAdapter has correct configuration", () => {
  const adapter = new CodexAdapter();
  assert.equal(adapter.name, "codex");
  assert.equal(adapter.command, "codex");
  assert.deepEqual(adapter.buildArgs("assignment.json"), [
    "exec",
    "--json",
    "Read assignment.json and perform only your assigned task.",
  ]);
});

test("CodexAdapter owns final-output capture arguments", () => {
  const adapter = new CodexAdapter();
  assert.deepEqual(
    adapter.configureFinalOutputCapture(
      ["exec", "--model", "gpt-test", "Read the prompt."],
      "/tmp/final.md",
    ),
    [
      "exec",
      "--model",
      "gpt-test",
      "--output-last-message",
      "/tmp/final.md",
      "Read the prompt.",
    ],
  );
});

test("CodexAdapter parses JSONL fixture", async () => {
  const adapter = new CodexAdapter();
  const fixturePath = path.join(__dirname, "fixtures/runners/codex.jsonl");
  const raw = await readFile(fixturePath, "utf-8");
  const events = adapter.parseOutput(raw, "test-run", "test-role");

  assert.ok(events.length > 0);
  assert.equal(events[0].type, "activity");
  assert.equal(events[0].runId, "test-run");
  assert.equal(events[0].roleId, "test-role");
});

test("CodexAdapter handles malformed JSON gracefully", () => {
  const adapter = new CodexAdapter();
  const events = adapter.parseOutput(
    "invalid json\n{broken\n",
    "test-run",
    "test-role",
  );
  assert.ok(events.length > 0);
  assert.equal(events[0].type, "activity");
  assert.equal(events[0].payload.kind, "activity");
});

test("CodexAdapter exposes emitted reasoning summaries and command progress", () => {
  const adapter = new CodexAdapter();
  const events = adapter.parseOutput(
    [
      JSON.stringify({
        type: "item.completed",
        item: { type: "reasoning", text: "Checking the packet contracts" },
      }),
      JSON.stringify({
        type: "item.started",
        item: { type: "command_execution", command: "pnpm test" },
      }),
      JSON.stringify({
        type: "item.completed",
        item: {
          type: "command_execution",
          command: "pnpm test",
          aggregated_output: "231 tests passed",
        },
      }),
    ].join("\n"),
    "run-1",
    "architect",
  );

  assert.deepEqual(
    events.map((event) => event.type),
    ["activity", "tool-call", "tool-output"],
  );
  assert.equal(
    events[0]?.payload.kind === "activity"
      ? events[0].payload.message
      : undefined,
    "Reasoning summary: Checking the packet contracts",
  );
});

test("CodexAdapter preserves provider failures as runner errors", () => {
  const adapter = new CodexAdapter();
  const [event] = adapter.parseOutput(
    `${JSON.stringify({
      type: "error",
      message: JSON.stringify({
        type: "error",
        status: 400,
        error: {
          type: "invalid_request_error",
          message: "The configured model is not supported for this account.",
        },
      }),
    })}\n`,
    "run-1",
    "backend",
  );
  assert.equal(event?.type, "error");
  assert.equal(
    event?.payload.kind === "error" ? event.payload.message : undefined,
    "The configured model is not supported for this account.",
  );
});

test("OpenCodeAdapter has correct configuration", () => {
  const adapter = new OpenCodeAdapter();
  assert.equal(adapter.name, "opencode");
  assert.equal(adapter.command, "opencode");
  assert.deepEqual(adapter.buildArgs("/tmp/assignment.json"), [
    "run",
    "--format",
    "json",
    "Read /tmp/assignment.json and perform only your assigned task.",
  ]);
});

test("OpenCodeAdapter parses JSON fixture", async () => {
  const adapter = new OpenCodeAdapter();
  const fixturePath = path.join(__dirname, "fixtures/runners/opencode.jsonl");
  const raw = await readFile(fixturePath, "utf-8");
  const events = adapter.parseOutput(raw, "test-run", "test-role");

  assert.ok(events.length > 0);
  assert.equal(events[0].runId, "test-run");
});

test("PiAdapter has correct configuration", () => {
  const adapter = new PiAdapter();
  assert.equal(adapter.name, "pi");
  assert.equal(adapter.command, "pi");
});

test("PiAdapter parses JSON fixture", async () => {
  const adapter = new PiAdapter();
  const fixturePath = path.join(__dirname, "fixtures/runners/pi.jsonl");
  const raw = await readFile(fixturePath, "utf-8");
  const events = adapter.parseOutput(raw, "test-run", "test-role");

  assert.ok(events.length > 0);
  assert.equal(events[0].runId, "test-run");
});

test("KiloAdapter has correct configuration", () => {
  const adapter = new KiloAdapter();
  assert.equal(adapter.name, "kilo");
  assert.equal(adapter.command, "kilo");
});

test("KiloAdapter parses JSON fixture", async () => {
  const adapter = new KiloAdapter();
  const fixturePath = path.join(__dirname, "fixtures/runners/kilo.jsonl");
  const raw = await readFile(fixturePath, "utf-8");
  const events = adapter.parseOutput(raw, "test-run", "test-role");

  assert.ok(events.length > 0);
  assert.equal(events[0].runId, "test-run");
});

test("createUnavailableEvent sets lifecycle state to unavailable", () => {
  const event = createUnavailableEvent(
    "codex",
    "not in PATH",
    "run-1",
    "backend",
  );
  assert.equal(event.type, "lifecycle");
  assert.equal(event.runId, "run-1");
  assert.equal(event.roleId, "backend");
  const payload = event.payload as LifecyclePayload;
  assert.equal(payload.state, "unavailable");
  assert.match(payload.message!, /codex/);
});

test("all adapters produce activity for malformed JSON input", () => {
  for (const adapter of [
    new CodexAdapter(),
    new OpenCodeAdapter(),
    new PiAdapter(),
    new KiloAdapter(),
  ]) {
    const events = adapter.parseOutput(
      "not json\n{broken\n",
      "run-1",
      "role-1",
    );
    assert.ok(
      events.length > 0,
      `${adapter.name} should handle malformed input`,
    );
    assert.equal(events[0].type, "activity");
    assert.equal(events[0].payload.kind, "activity");
  }
});

test("CodexAdapter incrementally parses JSONL split across chunks and captures final response", () => {
  const adapter = new CodexAdapter();
  const parser = adapter.createOutputParser("run", "role");
  const finalResponse = JSON.stringify({
    protocolVersion: "1.0",
    status: "completed",
    summary: "ok",
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
  });

  assert.equal(
    parser.push('{"type":"message","role":"assistant","content":"hel').length,
    0,
  );
  const events = parser.push(
    `lo"}
${JSON.stringify({ type: "final", content: finalResponse })}
`,
  );

  assert.ok(events.some((event) => event.type === "activity"));
  assert.match(parser.finalResponse ?? "", /"protocolVersion":"1.0"/);
});

test("registry capture accepts a direct AgentResponseV1 JSON object", () => {
  const response = JSON.stringify({
    protocolVersion: "1.0",
    status: "completed",
    summary: "done",
  });
  assert.equal(
    captureFinalResponse("opencode", "run", "role", `${response}\n`, "", ""),
    response,
  );
});
