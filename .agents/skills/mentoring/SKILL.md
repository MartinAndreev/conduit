---
name: mentoring
description: Walk through an implemented Conduit Go feature to teach Go-specific idioms, language mechanics, and architectural choices directly from the real code. Use when the engineer opts in after implementation or asks to learn how the Go code works; do not run automatically or modify code.
---

# Go mentoring walkthrough

Help engineers who are less familiar with Go understand what was implemented, how it works, and why specific Go patterns were chosen. Use the language of the current conversation unless explicitly requested otherwise. Educational only: do not modify code, run live/paid harness calls, or accept/block a feature.

## Ground in the implemented feature

- Inspect the actual code, types, tests, and relevant collaborators for the implemented feature rather than teaching abstract Go theory.
- Focus on Go concepts that genuinely appear in the change, such as package boundaries, struct composition and implicit interface satisfaction (`var _ Interface = (*Type)(nil)`), value vs. pointer receivers, explicit error handling and wrapping (`errors.Is`/`errors.As`), slice/map semantics, `context.Context` cancellation, goroutines/channels/synchronous mutex scopes (`internal/lockscope`), table-driven tests, or build tags.
- Skip concepts that are not present in the implementation. Do not turn the walkthrough into an exhaustive language reference.

## Structure the explanation

1. **What was built (briefly):** 2–4 sentences summarizing the feature's role and where it sits in the codebase.
2. **Go mechanics & idioms in this code:** 3–6 focused sections anchored to real files and line ranges. For each concept:
   - Point to the exact symbol/lines in the feature.
   - Explain what Go is doing under the hood and why this shape was chosen (and, when helpful, what pitfall it avoids).
   - Keep code excerpts short and free of secrets or raw credential-bearing logs.
3. **Architecture & testing takeaways:** Briefly show how the repository's rules/procedures/ports separation and deterministic Go tests (`t.Run`, fakes, `-race`) work in practice here.
4. **Open questions / next depth:** Invite the engineer to pick any part to dive deeper into (e.g., concurrency, memory/pointers, error boundaries, or testing patterns).

## Boundaries

- Do not load this skill automatically during implementation, audits, or feature walkthroughs; ask the engineer after implementing a feature whether they want to run it.
- Keep `feature-walkthrough` (flow map, Mermaid diagram, review hotspots) and `implementation-audit` (independent review) separate from mentoring.
