---
name: feature-walkthrough
description: Explain the full implemented Conduit Go feature for a human developer with a readable flow map, Mermaid diagram, code navigation, hotspots, and verification limits. Use for feature handoff or code walkthroughs, not as an audit verdict or implementation task.
---

# Feature walkthrough

Produce a concise, navigable explanation of the actual feature so developers can understand its flow and focus their code review on critical areas. Cover the whole feature, including relevant unchanged components, rather than summarizing only the latest diff. Do not edit implementation or decide acceptance.

## Ground the account

- Read repository `AGENTS.md` and the architecture and verification documents it routes to. Read the approved feature contract and inspect its entry points, procedures, dependencies, and tests.
- Identify the feature scope and code snapshot, including uncommitted changes. Ask if scope or intended behavior is materially unclear; do not invent it.
- Use the latest independent audit if available. Check whether its findings still apply to the current code; cite their status and preserve uncertainty. The audit is useful input, not a prerequisite: if none exists, say so and derive code hotspots without claiming independent review.
- Distinguish implemented behavior, planned behavior, and observed deviations. Do not rewrite a plan as though it were running code, or imply components are wired into the application when they are not.

## Human-oriented output

Use the language of the current conversation unless the user explicitly requests another output language. If delegated to a fresh agent, include the conversation language in its assignment. Scale detail to the feature, with an overview first and supporting navigation afterward:

- Explain what the feature enables, its boundaries, and what remains unavailable.
- Provide a short reading route through the actual entry point, flow owner, rules, reads, effects, and infrastructure. Link to verified files/symbols; avoid exhaustive file inventories.
- Include one main Mermaid flow or sequence diagram, choosing the form that best explains the behavior. Label nodes with understandable actions and identifiable code owners. Show consequential decisions and failure paths, with effect/transaction or asynchronous boundaries where relevant. Do not invent architecture to fit a diagram; use another small diagram only when the main one would become unreadable.
- Explain how facts become decisions and effects, using the repository's vocabulary. Make state changes, important data contracts, and partial-success behavior visible where they affect understanding.
- Highlight hotspots: code requiring careful human review, its invariants, consequences of changing it, and applicable audit findings. Clearly distinguish critical code with no known defect from known problems. Prioritize risk and decision value, not volume.
- State what was verified, by whom or from which evidence, what remains untested, known limitations, and decisions still required. Explain important design tradeoffs only when supported by code or recorded decisions; label inferred rationale.

Use plain language and small examples where they help. Do not load the personal mentoring skill automatically, turn this into a full language tutorial, or expose secrets/raw credential-bearing logs in examples.

Before handoff, verify links, diagram syntax/labels, and correspondence between the diagram and code. A walkthrough is not proof that an audit passed. If asked to save it, use the agreed feature-document destination; otherwise present it in the task. Do not invent a permanent documentation location.
