import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { ProjectDatabaseFactory } from "../../src/system/storage/factories/database-factories.js";
import { TursoRuntimeEventRepository } from "../../src/domains/runs/repositories/turso-runtime-event-repository.js";
import { TursoConduitResultRecordRepository } from "../../src/domains/runs/repositories/turso-conduit-result-record-repository.js";
import { TursoClarificationQuestionRepository } from "../../src/domains/refinement/repositories/turso-clarification-question-repository.js";
import { TursoHarnessRuntimeStateRepository } from "../../src/domains/runs/repositories/turso-harness-runtime-state-repository.js";
import type { ConduitResultRecordV1 } from "../../src/domains/runs/types/agent-protocol.js";

const response = {
  protocolVersion: "1.0" as const,
  status: "completed" as const,
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
};

test("runtime events and authoritative result records round-trip through state.db", async () => {
  const projectRoot = await mkdtemp(
    path.join(os.tmpdir(), "conduit-runtime-db-"),
  );
  const connection = await new ProjectDatabaseFactory(projectRoot).open();
  try {
    const events = new TursoRuntimeEventRepository(connection);
    await events.append({
      version: "1.0",
      sequence: 1,
      receivedAt: "2026-07-16T00:00:00.000Z",
      context: { runId: "run-1", roleId: "backend" },
      provenance: "runner-reported",
      type: "tool-call",
      payload: { state: "started", tool: "read" },
      native: { protocol: "exec-jsonl", nativeCorrelationId: "call-1" },
    });
    await events.append({
      version: "1.0",
      sequence: 1,
      receivedAt: "2026-07-16T00:00:00.500Z",
      context: { runId: "run-1", roleId: "backend" },
      provenance: "conduit-observed",
      type: "protocol-lifecycle",
      payload: { state: "session-restarted" },
      native: { protocol: "exec-jsonl" },
    });
    const persistedEvents = await events.loadByRole("run-1", "backend");
    assert.equal(persistedEvents[0]?.native?.nativeCorrelationId, "call-1");
    assert.deepEqual(
      persistedEvents.map((event) => event.sequence),
      [1, 2],
    );

    const records = new TursoConduitResultRecordRepository(connection);
    const record: ConduitResultRecordV1 = {
      recordVersion: "1.0",
      runId: "run-1",
      featureId: "008",
      taskId: null,
      assignmentId: "run-1:backend",
      role: "backend",
      runner: "codex",
      model: null,
      receivedAt: "2026-07-16T00:00:01.000Z",
      process: { exitCode: 0, acceptable: true, cancelled: false },
      observedChangedFiles: [],
      conduitObservedEvents: [],
      runnerReportedEvents: [],
      agentClaimedEvents: [],
      protocolValidation: { valid: true, issues: [] },
      semanticValidation: { valid: true, issues: [] },
      response,
    };
    await records.save(record);
    assert.deepEqual(await records.load("run-1", "backend"), record);
  } finally {
    await connection.close();
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("package, session, turn, and diagnostic metadata persist canonically", async () => {
  const projectRoot = await mkdtemp(
    path.join(os.tmpdir(), "conduit-session-db-"),
  );
  const connection = await new ProjectDatabaseFactory(projectRoot).open();
  try {
    const repository = new TursoHarnessRuntimeStateRepository(connection);
    await repository.savePackageVersion({
      id: "pkg-1",
      featureId: "008",
      packageHash: "a".repeat(64),
      inputs: ["spec.md"],
      createdAt: "2026-07-16T00:00:00.000Z",
    });
    assert.equal(
      (await repository.findPackageVersion("a".repeat(64)))?.id,
      "pkg-1",
    );
    await repository.saveSession({
      id: "session-1",
      featureId: "008",
      packageVersionId: "pkg-1",
      providerId: "codex-exec",
      harness: "codex",
      harnessVersion: "0.144.4",
      protocol: "exec-jsonl",
      status: "active",
      createdAt: "2026-07-16T00:00:00.000Z",
      updatedAt: "2026-07-16T00:00:00.000Z",
    });
    assert.equal(
      (await repository.loadSession("session-1"))?.protocol,
      "exec-jsonl",
    );
    await repository.saveTurn({
      id: "turn-1",
      sessionId: "session-1",
      assignmentId: "run-1:backend",
      kind: "assignment",
      status: "completed",
      startedAt: "2026-07-16T00:00:00.000Z",
      completedAt: "2026-07-16T00:00:01.000Z",
    });
    await repository.saveDiagnosticArtifact({
      id: "artifact-1",
      runId: "run-1",
      roleId: "backend",
      kind: "transcript",
      path: ".conduit/runs/run-1/backend.log",
      sizeBytes: 20,
      truncated: false,
      createdAt: "2026-07-16T00:00:00.000Z",
      expiresAt: "2026-07-23T00:00:00.000Z",
    });
  } finally {
    await connection.close();
    await rm(projectRoot, { recursive: true, force: true });
  }
});

test("clarifications are fingerprinted, answered, reminded once, then fail closed", async () => {
  const projectRoot = await mkdtemp(
    path.join(os.tmpdir(), "conduit-clarification-db-"),
  );
  const connection = await new ProjectDatabaseFactory(projectRoot).open();
  try {
    const repository = new TursoClarificationQuestionRepository(connection);
    const question = {
      id: "q1",
      question: "Choose A or B?",
      context: "No decision.",
      options: ["A", "B"],
      unblocker: "Choose one.",
    };
    const first = await repository.record("008", "001", [question]);
    assert.equal(first.unresolved.length, 1);
    await repository.answerUnresolved("008", "001", "Choose A");
    assert.equal((await repository.unresolved("008", "001")).length, 0);
    const repeated = await repository.record("008", "001", [question]);
    assert.equal(repeated.reminders.length, 1);
    await assert.rejects(
      repository.record("008", "001", [question]),
      /REPEATED_CLARIFICATION_LOOP/,
    );
  } finally {
    await connection.close();
    await rm(projectRoot, { recursive: true, force: true });
  }
});
