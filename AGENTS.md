# Conduit Go instruction registry

Read only the instruction files relevant to the current task, before doing the corresponding work. Follow links below; do not load every document or skill by default.

## Always applicable

- Ask Martin about missing material requirements or architectural decisions; do not decide them silently or treat silence as agreement.
- Implement only approved scope. The main assistant delegates implementation and executable tests to Sol, and may investigate, review, verify, and edit documentation itself.
- Do not commit unless requested. Preserve unrelated work and the original TypeScript repository.

## Instructions by task

| When | Read |
| --- | --- |
| Designing, implementing, or reviewing architecture/code | [Architecture](.agents/instructions/architecture.md) |
| Planning work, agreeing scope, or delegating to agents | [Collaboration](.agents/instructions/collaboration.md) |
| Validating changes, reviewing delivery, or handing off | [Verification](.agents/instructions/verification.md) |

Read the active feature's relevant documents under `specs/` and explicitly referenced planning documents before assigning or changing implementation. Surface conflicts before dependent work. Existing code does not establish architectural approval; these instructions do not authorize a bulk refactor.

## Skills

- Personal `agent-orchestration`: use when coordinating approved implementation through agents.
- Personal `mentoring`: use when explaining concepts or helping Martin learn. Martin is learning Go through this project; adapt explanations to that context and communicate in Bulgarian unless requested otherwise.
- These skills are independent. Neither requires loading the other. If unavailable, do not claim to have loaded them; repository rules still apply.
- [implementation-audit](.agents/skills/implementation-audit/SKILL.md): after feature implementation, use a fresh independent agent for advisory review of contracts, coding standards, architecture, and human readability. Highlight critical code for Martin; do not automatically fix, accept, or block the feature.
- [feature-walkthrough](.agents/skills/feature-walkthrough/SKILL.md): for a feature handoff or requested overview, explain the whole implementation with a Mermaid diagram, reading route, hotspots, and verification limits. Use current audit findings when available; an audit is not a prerequisite.
- [duplication-audit](.agents/skills/duplication-audit/SKILL.md): when analyzing repeated logic or scattered access to shared state/resources, identify changes requiring coordinated edits and recommend ownership/reuse boundaries. Advisory only; do not merge superficially similar code or automatically refactor.
- Load only the skill needed for the current task. Register future project-specific skills here with their purpose and trigger; do not scan or load all skills on every task.
