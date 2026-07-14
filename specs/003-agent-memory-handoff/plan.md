# Implementation plan

## Dependency on Feature 002

Feature 003 starts only after Feature 002 provides embedded Turso project/global databases, migration registry, repository infrastructure, redaction, configuration resolution, transactions, batching, shutdown, and standalone compatibility. Feature 003 consumes those facilities and contributes memory/indexing/handoff migrations through the 002 migration registry.

## Domain ownership

- `memory`: memory nodes, versions, proposals, evidence, embeddings, retrieval, feedback, promotion, lifecycle commands/queries, and embedding-provider port.
- `source` or `memory` repository-index subdomain: deterministic source indexing, source versions, evidence resolution, stale detection.
- `runs`: structured handoff envelope collection and process-boundary integration, while memory activation remains in the memory domain.
- `configuration`: memory budget, retrieval, embedding, promotion, and redaction settings resolved through 002 precedence.
- `system`: no new DB ownership; only uses 002 storage interfaces.

## Task groups

### Group 003-A: Contracts, schema, and lifecycle foundation

Deliverables:

- Define memory scopes, kinds, lifecycle states, proposal contracts, evidence references, handoff envelope contract, context-pack contract, and retrieval-attribution contract.
- Add project/global memory migrations using 002 migration infrastructure.
- Implement proposal validation, redaction, size limits, scope checks, duplicate/contradiction detection, create/merge/supersede/reject decisions, and audit records.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- memory-lifecycle`
- `pnpm --filter conduit-orchestrator typecheck`

### Group 003-B: Deterministic repository index and evidence invalidation

Deliverables:

- Add deterministic incremental indexer excluding ignored files, `.git`, `.conduit`, dependencies/vendor directories, generated output, binaries, oversized files, secrets, raw transcripts, and unredacted logs.
- Track path metadata, file kind, content hash, ownership, headings/symbols where deterministic, feature/contract associations, and exclusion reasons.
- Attach evidence hashes to memory and mark dependent memory stale when evidence changes.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- repository-index`
- `pnpm --filter conduit-orchestrator test -- evidence-invalidation`

### Group 003-C: Deterministic and lexical retrieval

Deliverables:

- Build query construction, mandatory deterministic context selection, eligibility filtering, lexical/exact candidate generation, rank fusion without embeddings, deduplication/diversity, contradiction blockers, and score explanations.
- Ensure stale/invalid/unauthorized memories are filtered before ranking.
- Persist retrieval attribution for selected memories.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- memory-retrieval`
- `pnpm --filter conduit-orchestrator test -- retrieval-attribution`

### Group 003-D: Optional embeddings and Turso vector retrieval

Deliverables:

- Define `EmbeddingProvider` port and local-first optional provider integration point.
- Build canonical retrieval document generation and hash-based embedding refresh.
- Store vector metadata and values using Turso vector-compatible BLOB representations.
- Add filtered linear cosine candidate generation and dimension/model/representation compatibility checks.
- Create labelled evaluation corpus and benchmark Float32 versus Float8, relevance, latency, DB size, standalone impact, CPU/memory, cold start, and batch generation.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- memory-vectors`
- `pnpm --filter conduit-orchestrator test -- embedding-benchmark`

### Group 003-E: Context-pack compiler and cache

Deliverables:

- Compile prompt-ready context packs with hard configurable budgets, mandatory-priority ordering, selected/omitted item records, estimated and actual prompt size, and no mid-claim truncation.
- Cache packs by task, role, resolved configuration, repository-index revision, evidence hashes, and contracts.
- Invalidate cache on relevant evidence, configuration, task-contract, role-guidance, handoff, or memory changes.
- Integrate run planning so agents receive context-pack files, not database access.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- context-packs`
- `pnpm --filter conduit-orchestrator test -- no-agent-db-access`

### Group 003-F: Structured handoffs and memory proposal ingestion

Deliverables:

- Validate versioned handoff envelopes from agent process results.
- Reject malformed, oversized, transcript-like, secret-bearing, or unauthorized handoffs.
- Store accepted envelopes and convert `suggestedMemories` to proposals only after validation/redaction.
- Preserve bounded predecessor handoffs for later context packs.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- handoffs`
- `pnpm --filter conduit-orchestrator test -- handoff-security`

### Group 003-G: Global promotion, feedback, and token-efficiency evaluation

Deliverables:

- Add explicit approval flow for global promotion.
- Generalize project memory into global lessons without raw paths/identifiers/evidence.
- Persist promotion provenance and separate global embeddings when enabled.
- Add retrieval feedback records and bounded ranking feedback behavior.
- Add measured token-efficiency tests across representative launches.

Completion checks:

- `pnpm --filter conduit-orchestrator test -- global-promotion`
- `pnpm --filter conduit-orchestrator test -- memory-feedback`
- `pnpm --filter conduit-orchestrator test -- token-efficiency`

## Migration order

Feature 003 migrations follow Feature 002 base migrations:

1. `003_0001_source_versions_index` for source versions/index metadata if not fully created by 002 primitives.
2. `003_0002_memory_nodes_versions_evidence` in project DB.
3. `003_0003_memory_proposals_relations_feedback` in project DB.
4. `003_0004_context_packs_and_items` in project DB.
5. `003_0005_handoff_envelopes` in project DB.
6. `003_0006_memory_embeddings` in project DB, optional provider-compatible columns enabled without requiring a provider.
7. `003_0007_global_promoted_memory` in global DB.
8. `003_0008_global_memory_embeddings` in global DB.

## Failure and recovery behavior

- Missing Feature 002 storage capabilities block Feature 003 startup with a clear dependency error.
- Indexing failures record path-level diagnostics and do not activate unverifiable memories.
- Embedding failures mark embedding metadata failed/retryable and do not roll back valid relational memory.
- Dimension/model/representation mismatches schedule re-embedding and exclude incompatible vectors.
- Contradictory active candidates surface a blocker in context-pack diagnostics.
- Handoff validation failures preserve sanitized diagnostics and do not create memory proposals.
- Global promotion rejection leaves project memory unchanged and records the decision.

## Verification strategy

- Unit tests for lifecycle state transitions, proposal validation, redaction, canonical document hashes, rank fusion, token budgeting, and promotion generalization.
- Integration tests using temporary project/global databases from Feature 002.
- Golden tests for deterministic indexing and context-pack output.
- Labelled retrieval evaluation corpus with expected top-k membership and precision/recall thresholds.
- Process-boundary tests proving agents receive only context-pack files and return handoff envelopes.
