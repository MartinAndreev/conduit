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

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test("CodexAdapter has correct configuration", () => {
  const adapter = new CodexAdapter();
  assert.equal(adapter.name, "codex");
  assert.equal(adapter.command, "codex");
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

test("OpenCodeAdapter has correct configuration", () => {
  const adapter = new OpenCodeAdapter();
  assert.equal(adapter.name, "opencode");
  assert.equal(adapter.command, "opencode");
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
