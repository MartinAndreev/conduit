import type { ArchitectPreferences } from "./architect-preferences.js";

export function architectExecutionContract(
  preferences: ArchitectPreferences,
): string {
  const effort = {
    standard:
      "Inspect directly relevant code and tests. Produce the smallest sound design that fits established repository boundaries.",
    thorough:
      "Trace relevant call paths, state, configuration, tests, and integration boundaries. Compare viable designs, record consequential tradeoffs, and choose the simplest design that fully satisfies the request.",
    exhaustive:
      "Map end-to-end control and data flow, state transitions, failure and concurrency modes, trust boundaries, migrations, compatibility risks, and verification coverage. Challenge the preferred design against credible alternatives and resolve every implementation ambiguity that repository evidence can answer.",
  }[preferences.effort];
  const detail = {
    concise:
      "State the chosen design, essential contracts, acceptance criteria, and verification without optional elaboration.",
    "implementation-ready":
      "Specify component responsibilities, affected files or modules, interfaces, validation boundaries, state changes, failure behavior, compatibility, and verification clearly enough for one implementation pass.",
    "implementation-blueprint":
      "Provide a concrete blueprint: component responsibilities, affected files or modules, contracts, ordered control and data flow, state lifecycle, validation, error and recovery behavior, concurrency or atomicity where relevant, compatibility, edge cases, and exact verification. Add concise pseudocode or signatures only where they remove implementation ambiguity.",
  }[preferences.detailLevel];
  return `# Architect execution policy (immutable)

Selected effort: ${preferences.effort}. ${effort}
Selected detail: ${preferences.detailLevel}. ${detail}

Apply language-independent design discipline where relevant: prefer simple cohesive components, explicit boundaries and contracts, one owner for each state transition and source of truth, validation at trust boundaries, deterministic behavior, safe retry or idempotency when operations can repeat, least privilege, backward compatibility, observable failures, and testable seams. Reuse established repository patterns unless evidence justifies a change. Do not introduce abstractions, patterns, or extensibility without a concrete requirement; state material tradeoffs briefly.

Effort and detail control the depth of investigation and the specificity of the handoff. They are application settings, not project guidance, and must be followed regardless of provider.`;
}
