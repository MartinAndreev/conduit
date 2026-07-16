import assert from "node:assert/strict";
import test from "node:test";
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { CommunicationProviderId } from "../../src/system/communication/enums/communication-provider-id.js";
import { CliJsonCommunicationProvider } from "../../src/system/communication/providers/cli-json-communication-provider.js";
import { consumeCommunicationStream } from "../../src/system/communication/services/consume-communication-stream.js";
import { parseNativeEvent } from "../../src/system/communication/services/native-event-parser.js";
import { createAgentAssignmentV1 } from "../../src/domains/runs/factories/agent-assignment-factory.js";
import { AgentRoleKind } from "../../src/domains/roles/enums/agent-role-kind.js";
import { agentResponseOutputSchema } from "../../src/system/communication/services/agent-response-output-schema.js";

const fixtureRoot = path.join(
  import.meta.dirname,
  "..",
  "fixtures",
  "communication",
);

test("Codex output schema declares strict types and array item schemas", () => {
  assert.deepEqual(agentResponseOutputSchema.properties.protocolVersion, {
    type: "string",
    const: "1.0",
  });
  for (const property of [
    "artifacts",
    "findings",
    "verification",
    "decisions",
    "blockers",
    "questions",
    "risks",
    "evidence",
    "memoryProposals",
    "globalPromotionProposals",
  ] as const) {
    const schema = agentResponseOutputSchema.properties[property];
    assert.equal(schema.type, "array");
    assert.equal(schema.items.type, "object");
    assert.equal(schema.items.additionalProperties, false);
  }
});

test("captured fixture metadata is complete and sanitized", async () => {
  const metadata = JSON.parse(
    await readFile(path.join(fixtureRoot, "metadata.json"), "utf8"),
  ) as {
    fixtures: {
      file: string;
      harness: string;
      version: string;
      command: string;
      protocol: string;
      capturedAt: string;
      sanitized: boolean;
    }[];
  };
  assert.equal(metadata.fixtures.length, 8);
  for (const fixture of metadata.fixtures) {
    assert.ok(
      fixture.harness &&
        fixture.version &&
        fixture.command &&
        fixture.protocol &&
        fixture.capturedAt,
    );
    assert.equal(fixture.sanitized, true);
    const content = await readFile(
      path.join(fixtureRoot, fixture.file),
      "utf8",
    );
    assert.doesNotMatch(
      content,
      /encrypted_content|thinkingSignature|\/home\/martin/,
    );
  }
});

test("captured events expose lifecycle, usage, final content, and stable correlation", async () => {
  const codex = (
    await readFile(path.join(fixtureRoot, "codex-exec-0.144.4.jsonl"), "utf8")
  )
    .trim()
    .split("\n")
    .flatMap((line) => parseNativeEvent("codex", JSON.parse(line)));
  assert.ok(codex.some((event) => event.type === "usage"));
  assert.ok(codex.some((event) => event.correlationId === "ITEM_REDACTED"));

  const opencode = (
    await readFile(path.join(fixtureRoot, "opencode-run-1.17.18.jsonl"), "utf8")
  )
    .trim()
    .split("\n")
    .flatMap((line) => parseNativeEvent("opencode", JSON.parse(line)));
  assert.ok(opencode.some((event) => event.type === "usage"));

  const kilo = (
    await readFile(path.join(fixtureRoot, "kilo-run-7.4.9-error.jsonl"), "utf8")
  )
    .trim()
    .split("\n")
    .flatMap((line) => parseNativeEvent("kilo", JSON.parse(line)));
  assert.ok(kilo.some((event) => event.type === "native-error"));

  for (const fixture of [
    "opencode-acp-1.17.18.jsonl",
    "kilo-acp-7.4.9.jsonl",
  ]) {
    const records = (await readFile(path.join(fixtureRoot, fixture), "utf8"))
      .trim()
      .split("\n")
      .map((line) => JSON.parse(line) as Record<string, unknown>);
    assert.ok(
      records.some(
        (record) =>
          (record.result as { protocolVersion?: number } | undefined)
            ?.protocolVersion === 1,
      ),
    );
    assert.ok(records.some((record) => record.method === "session/update"));
  }

  const piRpc = (
    await readFile(path.join(fixtureRoot, "pi-rpc-0.80.8.jsonl"), "utf8")
  )
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as Record<string, unknown>);
  assert.ok(piRpc.some((record) => record.type === "agent_settled"));
  assert.ok(
    piRpc.some(
      (record) =>
        record.type === "response" &&
        record.command === "get_last_assistant_text",
    ),
  );

  const response = {
    protocolVersion: "1.0",
    status: "completed",
    summary: "ok",
  };
  assert.equal(
    parseNativeEvent("codex", response)[0]?.finalResponseCandidate,
    JSON.stringify(response),
  );
});

test("CLI JSON provider handles arbitrary chunks, malformed and oversized records, and final response", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "conduit-provider-"));
  const executable = path.join(directory, "fake-opencode");
  const response = JSON.stringify({
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
  await writeFile(
    executable,
    `#!/usr/bin/env node
if (process.argv.includes("--version")) { console.log("1.0.0"); process.exit(0); }
process.stdout.write('{"type":"text","part":{"type":"text","id":"call-1","text":"hel');
setTimeout(() => {
  process.stdout.write('lo"}}\\n');
  process.stdout.write('not-json\\n');
  process.stdout.write('x'.repeat(256100));
  process.stdout.write('\\n');
  process.stdout.write(${JSON.stringify(response + "\n")});
}, 5);
`,
  );
  await chmod(executable, 0o755);
  try {
    const provider = new CliJsonCommunicationProvider({
      id: CommunicationProviderId.OpenCodeJson,
      runner: "opencode",
      protocol: "run-json",
      executableCandidates: [executable],
      verifiedVersions: ["1.0.0"],
      finalResponseCapture: "json-fallback",
      buildArgs: () => [],
    });
    const assignment = createAgentAssignmentV1({
      assignmentId: "run-1:backend",
      role: "backend",
      roleKind: AgentRoleKind.Implementation,
      objective: "test provider framing",
      ownedPaths: [],
      contextReferences: [],
      acceptanceCriteria: ["parse"],
      contracts: ["specs"],
    });
    const session = await provider.createSession({
      assignment,
      projectRoot: directory,
      workspaceRoot: directory,
      runner: "opencode",
    });
    await session.start();
    await session.submit(assignment);
    const events: import("../../src/system/communication/types/runtime-event.js").ConduitRuntimeEvent[] =
      [];
    const terminal = await consumeCommunicationStream(
      session.events,
      async (event) => {
        events.push(event);
      },
    );
    await session.close();
    assert.equal(terminal.finalResponseCandidate, response);
    assert.deepEqual(
      events.map((event) => event.sequence),
      events.map((_, index) => index + 1),
    );
    assert.ok(
      events.some((event) => event.native?.nativeCorrelationId === "call-1"),
    );
    assert.ok(
      events.some((event) => event.payload.code === "MALFORMED_NATIVE_RECORD"),
    );
    assert.ok(
      events.some((event) => event.payload.code === "OVERSIZED_NATIVE_RECORD"),
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
