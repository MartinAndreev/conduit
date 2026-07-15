import type { AgentResponseV1 } from "../types/agent-protocol.js";

export function renderResearchReport(response: AgentResponseV1): string {
  const lines = ["# Research context", "", response.summary, ""];
  if (response.findings.length) {
    lines.push("## Findings", "");
    for (const finding of response.findings) lines.push(`- **${finding.severity}/${finding.category}** ${finding.message} (evidence: ${finding.evidence.join(", ")})`);
    lines.push("");
  }
  if (response.questions.length) {
    lines.push("## Questions", "");
    for (const question of response.questions) lines.push(`- ${question.question} — smallest unblocker: ${question.smallestUnblocker}`);
    lines.push("");
  }
  if (response.risks.length) {
    lines.push("## Risks", "");
    for (const risk of response.risks) lines.push(`- **${risk.category}** ${risk.risk}; mitigation: ${risk.mitigation}`);
    lines.push("");
  }
  return `${lines.join("\n").trim()}\n`;
}
