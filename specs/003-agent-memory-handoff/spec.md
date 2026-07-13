# Agent memory and handoff

## Outcome

Provide optional, local, bounded repository memory and role handoffs after embedded Turso persistence exists.

## Acceptance criteria

- [ ] Deterministic indexing excludes secrets, ignored paths, binaries, and raw transcripts.
- [ ] Memory observations are project-scoped, evidence-backed, confidence-labelled, and invalidated when source evidence changes.
- [ ] Role handoffs respect ownership and strict size budgets.
- [ ] Architect curation is explicit and cost-bearing; it never runs by default.
- [ ] No cloud, vector database, embedding service, or cross-project memory is introduced.
