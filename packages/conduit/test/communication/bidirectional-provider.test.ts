import assert from "node:assert/strict";
import test from "node:test";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { BidirectionalCommunicationProvider } from "../../src/system/communication/providers/bidirectional-communication-provider.js";
import { CommunicationProviderId } from "../../src/system/communication/enums/communication-provider-id.js";
import { consumeCommunicationStream } from "../../src/system/communication/services/consume-communication-stream.js";
import { createAgentAssignmentV1 } from "../../src/domains/runs/factories/agent-assignment-factory.js";
import { AgentRoleKind } from "../../src/domains/roles/enums/agent-role-kind.js";

const runtimeVersion = process.versions["bun"] ?? process.versions.node;

const finalResponse = JSON.stringify({
  protocolVersion: "1.0",
  status: "completed",
  summary: "fixture-ok",
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

function assignment() {
  return createAgentAssignmentV1({
    assignmentId: "run-1:reviewer",
    role: "reviewer",
    roleKind: AgentRoleKind.Reviewer,
    objective: "exercise the native protocol",
    ownedPaths: [],
    contextReferences: [],
    acceptanceCriteria: ["capture a final response"],
    contracts: ["specs"],
  });
}

test("ACP stdio negotiates, configures a model, streams chunks, and returns the prompt result", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "conduit-acp-"));
  const executable = path.join(directory, "fake-acp");
  await writeFile(
    executable,
    `#!/usr/bin/env node
let buffer = "";
let promptId;
const send = (value) => console.log(JSON.stringify(value));
const finishPrompt = () => {
  send({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "SESSION_REDACTED", update: { sessionUpdate: "agent_thought_chunk", messageId: "thought-1", content: { type: "text", text: "Read " } } } });
  send({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "SESSION_REDACTED", update: { sessionUpdate: "agent_thought_chunk", messageId: "thought-1", content: { type: "text", text: "package.json" } } } });
  send({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "SESSION_REDACTED", update: { sessionUpdate: "tool_call", toolCallId: "call-1", title: "read", rawInput: { file: "package.json" } } } });
  send({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "SESSION_REDACTED", update: { sessionUpdate: "tool_call_update", toolCallId: "call-1", title: "read", status: "completed", content: [{ type: "content", content: { type: "text", text: "{}" } }] } } });
  send({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "SESSION_REDACTED", update: { sessionUpdate: "agent_message_chunk", messageId: "message-1", content: { type: "text", text: ${JSON.stringify(finalResponse)} } } } });
  send({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "SESSION_REDACTED", update: { sessionUpdate: "usage_update", used: 10, size: 100 } } });
  send({ jsonrpc: "2.0", id: promptId, result: { stopReason: "end_turn" } });
};
const handle = (input) => {
  if (input.method === "initialize") return send({ jsonrpc: "2.0", id: input.id, result: { protocolVersion: 1 } });
  if (input.method === "session/new") return send({ jsonrpc: "2.0", id: input.id, result: { sessionId: "SESSION_REDACTED", configOptions: [{ id: "model", options: [{ value: "cheap/model" }] }, { id: "effort" }] } });
  if (input.method === "session/set_config_option") return send({ jsonrpc: "2.0", id: input.id, result: { configOptions: [] } });
  if (input.method === "session/prompt") {
    promptId = input.id;
    return send({ jsonrpc: "2.0", id: 99, method: "session/request_permission", params: { sessionId: "SESSION_REDACTED", toolCall: { locations: [{ path: "package.json" }] }, options: [{ optionId: "once", kind: "allow_once" }, { optionId: "reject", kind: "reject_once" }] } });
  }
  if (input.id === 99 && input.result?.outcome?.optionId === "once") finishPrompt();
};
process.stdin.on("data", (chunk) => {
  buffer += chunk;
  let newline;
  while ((newline = buffer.indexOf("\\n")) >= 0) {
    const line = buffer.slice(0, newline);
    buffer = buffer.slice(newline + 1);
    if (line) handle(JSON.parse(line));
  }
});
`,
  );
  await chmod(executable, 0o755);
  try {
    const provider = new BidirectionalCommunicationProvider({
      id: CommunicationProviderId.OpenCodeAcp,
      runner: "opencode",
      protocol: "acp-stdio",
      executableCandidates: [process.execPath],
      verifiedVersions: [runtimeVersion],
      buildArgs: () => [executable],
    });
    const input = assignment();
    const session = await provider.createSession({
      assignment: input,
      projectRoot: directory,
      workspaceRoot: directory,
      runner: "opencode",
      model: "cheap/model",
      effort: "low",
    });
    await session.start();
    await session.submit(input);
    const events: import("../../src/system/communication/types/runtime-event.js").ConduitRuntimeEvent[] =
      [];
    const terminal = await consumeCommunicationStream(
      session.events,
      async (event) => {
        events.push(event);
      },
    );
    await session.close();
    assert.equal(terminal.status, "completed");
    assert.equal(terminal.finalResponseCandidate, finalResponse);
    assert.ok(events.some((event) => event.type === "usage"));
    assert.ok(
      events.some(
        (event) =>
          event.type === "permission" && event.payload.state === "approved",
      ),
    );
    assert.ok(
      events.some(
        (event) =>
          event.type === "agent-activity" &&
          event.payload.summary === "Read package.json",
      ),
    );
    assert.ok(
      events.some(
        (event) =>
          event.type === "tool-call" &&
          event.payload.input === '{"file":"package.json"}',
      ),
    );
    assert.doesNotMatch(JSON.stringify(events), /\[object Object\]/);
    assert.equal(session.nativeSessionId, "SESSION_REDACTED");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("Pi RPC waits for agent_settled and captures the native last assistant response", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "conduit-pi-rpc-"));
  const executable = path.join(directory, "fake-pi");
  await writeFile(
    executable,
    `#!/usr/bin/env node
if (process.argv.includes("--version")) { console.log("0.80.8"); process.exit(0); }
let buffer = "";
process.stdin.on("data", (chunk) => { buffer += chunk; let newline; while ((newline = buffer.indexOf("\\n")) >= 0) { const line = buffer.slice(0, newline); buffer = buffer.slice(newline + 1); if (!line) continue; const input = JSON.parse(line);
if (input.type === "get_state") console.log(JSON.stringify({id:input.id,type:"response",command:"get_state",success:true,data:{sessionId:"SESSION_REDACTED"}}));
else if (input.type === "prompt") { console.log(JSON.stringify({id:input.id,type:"response",command:"prompt",success:true})); console.log(JSON.stringify({type:"agent_start"})); console.log(JSON.stringify({type:"message_end",message:{role:"assistant",content:[{type:"text",text:${JSON.stringify(finalResponse)}}]}})); console.log(JSON.stringify({type:"agent_settled"})); }
else if (input.type === "get_last_assistant_text") console.log(JSON.stringify({id:input.id,type:"response",command:"get_last_assistant_text",success:true,data:{text:${JSON.stringify(finalResponse)}}}));
}});
`,
  );
  await chmod(executable, 0o755);
  try {
    const provider = new BidirectionalCommunicationProvider({
      id: CommunicationProviderId.PiRpc,
      runner: "pi",
      protocol: "rpc-stdio",
      executableCandidates: [process.execPath],
      verifiedVersions: [runtimeVersion],
      buildArgs: () => [executable],
    });
    const input = assignment();
    const session = await provider.createSession({
      assignment: input,
      projectRoot: directory,
      workspaceRoot: directory,
      runner: "pi",
    });
    await session.start();
    await session.submit(input);
    const terminal = await consumeCommunicationStream(
      session.events,
      async () => {},
    );
    await session.close();
    assert.equal(terminal.status, "completed");
    assert.equal(terminal.finalResponseCandidate, finalResponse);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
