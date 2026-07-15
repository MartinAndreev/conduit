import { test } from "bun:test";
import assert from "node:assert/strict";
import type { RunnerEvent } from "../src/domains/runs/types/runner-events.js";
import { RunnerEventProvenance } from "../src/domains/runs/enums/runner-event-provenance.js";
import {
  extractFinalResponse,
  JsonLineOutputParser,
} from "../src/system/runners/jsonl-parser.js";

function activity(message: string): RunnerEvent {
  return {
    type: "activity",
    provenance: RunnerEventProvenance.RunnerReported,
    runId: "run",
    roleId: "role",
    timestamp: "2026-01-01T00:00:00.000Z",
    payload: { kind: "activity", message },
  };
}

test("JsonLineOutputParser buffers JSONL split across arbitrary chunks", () => {
  const parser = new JsonLineOutputParser(
    (line) => [activity(JSON.parse(line).message as string)],
    (line) => activity(`fallback:${line}`),
  );

  assert.deepEqual(parser.push('{"message":"hel'), []);
  const events = parser.push('lo"}\n{"message":"world"}');

  assert.equal(events.length, 1);
  assert.equal(events[0]?.payload.kind, "activity");
  assert.deepEqual(events[0]?.payload, { kind: "activity", message: "hello" });

  const flushed = parser.flush();
  assert.equal(flushed.length, 1);
  assert.deepEqual(flushed[0]?.payload, { kind: "activity", message: "world" });
});

test("JsonLineOutputParser captures final response from native event fields", () => {
  const parser = new JsonLineOutputParser(
    () => [],
    (line) => activity(`fallback:${line}`),
  );

  parser.push(
    '{"type":"final","content":"{\\"protocolVersion\\":\\"1.0\\"}"}\n',
  );

  assert.equal(parser.finalResponse, '{"protocolVersion":"1.0"}');
});

test("JsonLineOutputParser routes malformed lines through fallback", () => {
  const parser = new JsonLineOutputParser(
    () => [],
    (line) => activity(`fallback:${line}`),
  );

  const events = parser.push("not-json\n");

  assert.equal(events.length, 1);
  assert.deepEqual(events[0]?.payload, {
    kind: "activity",
    message: "fallback:not-json",
  });
});

test("extractFinalResponse supports runner final field variants", () => {
  assert.equal(
    extractFinalResponse({ final_response: '{"protocolVersion":"1.0"}' }),
    '{"protocolVersion":"1.0"}',
  );
  assert.equal(
    extractFinalResponse({
      role: "assistant",
      content: '{"status":"completed"}',
    }),
    '{"status":"completed"}',
  );
  assert.equal(
    extractFinalResponse({ type: "message", content: "plain" }),
    undefined,
  );
});
