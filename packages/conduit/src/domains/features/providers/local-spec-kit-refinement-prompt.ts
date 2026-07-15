import path from "node:path";
import type { Feature } from "../types/feature.js";
import { DEFAULT_ARCHITECT_PREFERENCES } from "@domains/refinement/types/architect-preferences.js";
import { architectExecutionContract } from "@domains/refinement/types/architect-execution-contract.js";
import { localSpecKitArchitectContract } from "./local-spec-kit-role-contract.js";

export function localSpecKitRefinementPrompt(
  feature: Feature,
  story: string,
  additionalContext = `${architectExecutionContract(DEFAULT_ARCHITECT_PREFERENCES)}\n\n${localSpecKitArchitectContract()}`,
  questionsPath = path.join(feature.directory, "questions.md"),
): string {
  const clarificationsFile = path.join(feature.directory, "clarifications.md");
  return `Turn the feature request into a compact, implementation-ready Local Spec Kit packet. Update packet artifacts themselves; never write an architect prompt, mandate, role description, instructions for a future architect, or meta commentary into feature files.

${additionalContext}

# Feature request

${story}

Read repository evidence and ${clarificationsFile} when it exists; recorded answers are product decisions. Update ${path.join(feature.directory, "spec.md")}, ${path.join(feature.directory, "plan.md")}, ${path.join(feature.directory, "tasks.md")}, ${path.join(feature.directory, "test-cases.md")}, and only necessary files under ${path.join(feature.directory, "contracts")}.

Write concrete artifacts: observable acceptance criteria, necessary contracts, tasks assigned to configured role ownership, and QA cases. Prefer tables and short bullets; omit repeated boilerplate and narrative. Use pseudocode or signatures only when they remove implementation ambiguity.

Never invent repository facts, APIs, or product decisions. If a material decision remains unclear after investigation, stop without updating the packet. Return AgentResponseV1 with status \`needs_input\` and put every decision in structured \`questions[]\`, including the complete question, why it matters, repository context, concrete options with consequences, and the smallest unblocker. Conduit will render that response to ${questionsPath}; do not create, modify, delete, or report that run-state file as an artifact. Do not update the handoff until the questions are answered.`;
}
