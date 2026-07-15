import type { AgentResponseV1 } from "../types/agent-protocol.js";

export function renderClarificationQuestions(
  response: AgentResponseV1,
): string {
  const lines = ["# Architect clarification questions", ""];
  response.questions.forEach((question, index) => {
    lines.push(`## ${index + 1}. ${question.question}`, "");
    lines.push(question.context, "");
    lines.push(`Why it matters: ${question.whyItMatters}`, "");
    lines.push("Options:", "");
    for (const option of question.options) lines.push(`- ${option}`);
    lines.push("", `Smallest unblocker: ${question.smallestUnblocker}`, "");
  });
  return `${lines.join("\n").trim()}\n`;
}
