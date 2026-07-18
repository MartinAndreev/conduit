import assert from "node:assert/strict";
import test from "node:test";
import { agentResponseContractPrompt } from "../../src/domains/runs/assets/agent-response-contract.js";
import { AgentRoleKind } from "../../src/domains/roles/enums/agent-role-kind.js";
import { createAgentAssignmentV1 } from "../../src/domains/runs/factories/agent-assignment-factory.js";
import { agentAssignmentPrompt } from "../../src/system/communication/services/agent-assignment-prompt.js";

function assignment() {
  return createAgentAssignmentV1({
    assignmentId: "run-1:qa",
    role: "qa",
    roleKind: AgentRoleKind.QualityAssurance,
    objective: "verify the approved acceptance criteria",
    ownedPaths: ["tests"],
    contextReferences: ["specs/001/test-cases.md"],
    acceptanceCriteria: ["Return a valid AgentResponseV1."],
    contracts: ["agent-response-v1.schema.json"],
  });
}

test("assignment prompt places the authoritative final-response contract after the assignment", () => {
  const input = assignment();
  const prompt = agentAssignmentPrompt(input);
  const assignmentPosition = prompt.indexOf(JSON.stringify(input));
  const contract = agentResponseContractPrompt();
  const contractPosition = prompt.indexOf(contract);

  assert.ok(assignmentPosition >= 0);
  assert.ok(contractPosition > assignmentPosition);
  assert.ok(prompt.endsWith(contract));
  assert.match(
    prompt,
    /current process working directory is the only workspace root/,
  );
  assert.match(prompt, /Never resolve them from a parent run directory/);
  assert.match(prompt, /Do not wrap it in Markdown fences/);
  assert.match(prompt, /"protocolVersion": "1\.0"/);
  assert.match(prompt, /"status": "completed \| partial/);
  assert.match(prompt, /verdict: \{ "decision":/);
  assert.match(prompt, /verification\[\]: \{ "operation":/);
});

test("ACP assignment prompt requires tool submission instead of prose JSON", () => {
  const prompt = agentAssignmentPrompt(assignment(), {
    toolName: "conduit_submit_agent_response",
  });
  assert.match(prompt, /calling the conduit_submit_agent_response tool/);
  assert.match(prompt, /final action/);
  assert.match(prompt, /correct every reported field and call it again/);
  assert.ok(prompt.endsWith(agentResponseContractPrompt("tool")));
  assert.doesNotMatch(prompt, /Return only one valid JSON object/);
});
