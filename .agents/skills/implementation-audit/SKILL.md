---
name: implementation-audit
description: Independently review a Conduit Go feature implementation against its approved contract, coding standards, architecture, and human readability. Use after implementation to recommend improvements and identify code for focused human review; does not accept, block, or modify the feature.
---

# Implementation audit

## Purpose and independence

Provide an evidence-based advisory review before the human review, with the most consequential code first. The responsible human reviewer decides acceptance. Neither findings nor the absence of findings automatically block or approve delivery. A hotspot is important code to inspect, not necessarily a defect.

Run the review in a fresh agent that did not implement the feature. The orchestrator supplies a bounded assignment without inherited conversation history (use `fork_turns: "none"` when supported): this skill, repository path, approved feature documents, exact implementation scope, revision/diff or identified uncommitted files, and permitted validation. Do not initially supply the implementer's conclusions, earlier audit verdicts, or a preferred answer. After the independent pass, compare relevant implementation notes if available and distinguish newly learned evidence.

If already assigned as that fresh reviewer, perform the audit; do not recursively delegate another audit. If a fresh context is unavailable, disclose the limitation and ask how to proceed instead of claiming independence. The Sol delegation agreement remains applicable; this skill does not authorize paid/live harness experiments or outside communication.

## Load only relevant evidence

- Read the repository `AGENTS.md`, then its architecture and verification instructions. Read collaboration instructions if coordinating agents.
- Read the approved feature specification, applicable task group, contracts, and test cases. Inspect actual code, relevant dependencies/callers, and tests, not only the diff or implementation summary.
- Establish the code snapshot and scope before review. Uncommitted work is valid input; do not require a commit. If the feature or comparison boundary is unclear, ask. Record changes made concurrently and qualify conclusions when the reviewed snapshot changes.
- Missing or conflicting requirements are questions, not rules to invent. Link to the applicable rule when reporting a violation.

## Review lenses

- Contract coverage: expected behavior, omissions, unapproved additions, and compatibility with surrounding code.
- Project architecture: domain ownership, decision results, effect boundaries, procedure ownership, wrapper ordering, and justified shared skeletons as defined by the linked instructions. Do not copy those rules into a second policy or impose an unapproved package layout.
- Human traceability: start from a real entry point and explain the principal flow and a consequential failure path with code links. Identify where naming, hidden state, scattered decisions, indirect calls, excessive fragmentation, or overly broad functions make a human lose the thread. Function/file counts alone are not findings.
- Correctness where applicable: invariant preservation, partial success, error propagation, context cancellation, concurrency, transaction scope, uncertain delivery, and retry safety. Prioritize actual feature risks rather than applying every possible checklist item.
- Evidence: inspect whether tests exercise observable behavior and important failures. Run authorized, relevant checks when useful; distinguish checks personally run, inspected tests, and results reported by others. Never infer correctness merely from a passing suite.

Do not edit implementation or tests, fix findings, or commit. Propose changes as separate work. If reproducing an issue requires mutations beyond disposable test artifacts or external side effects, obtain task-specific authorization first.

## Report for the human reviewer

Use the language of the current conversation unless the user explicitly requests another output language. When delegating to a fresh agent, include the conversation language in its assignment. Keep the first section short and useful for focused human review:

1. **Where to focus:** prioritized hotspots with file/symbol links, the invariant or consequence at stake, what is known, what remains uncertain, and what the human reviewer should inspect or decide. Include consequential areas even when no defect was found; do not manufacture defects to fill the list.
2. **Findings:** separate evidenced defects/rule violations, improvement suggestions, and open questions. Each finding states impact-based severity, concrete trigger/evidence, affected code, consequence, applicable contract/rule when relevant, and an actionable recommendation. Mark uncertainty explicitly. Stylistic preferences alone are not violations.
3. **Flow and coverage:** a brief human-readable account of the flow, reviewed scope/snapshot, checks actually run and results, unexamined areas, and validation limitations. Say when no findings were established; do not call that proof of correctness.

Do not issue an acceptance or blocking verdict. If asked to save the report, use the agreed feature-document destination; otherwise return it in the task. Recommendations can inform a separately authorized correction task and re-review.
