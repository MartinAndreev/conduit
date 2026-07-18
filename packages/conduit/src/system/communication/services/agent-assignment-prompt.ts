import { agentResponseContractPrompt } from "../../../domains/runs/assets/agent-response-contract.js";
import type { AgentAssignmentV1 } from "../../../domains/runs/types/agent-protocol.js";

export function agentAssignmentPrompt(
  assignment: AgentAssignmentV1,
  delivery?: { readonly toolName: string },
): string {
  const responseDelivery = delivery
    ? [
        `Submit the final AgentResponseV1 by calling the ${delivery.toolName} tool.`,
        "The tool call must be your final action. Do not print the response as prose or a JSON code block.",
        "If the tool reports validation errors, correct every reported field and call it again in this same turn.",
      ].join(" ")
    : "Return the final AgentResponseV1 as the final assistant response.";
  return [
    "Perform only this authoritative AgentAssignmentV1. The current process working directory is the only workspace root. Resolve every relative contextReferences and contracts path from that directory. Never resolve them from a parent run directory or access a sibling role worktree. Read the referenced context before acting.",
    JSON.stringify(assignment),
    responseDelivery,
    agentResponseContractPrompt(delivery ? "tool" : "assistant-response"),
  ].join("\n\n");
}
