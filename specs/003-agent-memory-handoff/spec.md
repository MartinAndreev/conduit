# Feature 003: Shared agent memory and bounded handoffs

## Outcome

Conduit becomes a context compiler backed by Turso. It prepares bounded, evidence-backed context packs for agents and accepts bounded structured handoff envelopes from agents. Agents never consume the database, raw project history, raw transcripts, or unbounded repository RAG output.

Feature 003 depends on Feature 002 for project/global database ownership, migrations, configuration resolution, redaction primitives, repositories, transactions, batching, and standalone Turso compatibility. Feature 003 owns repository indexing, memory lifecycle, retrieval, optional vector participation, context-pack compilation, handoff validation, retrieval attribution, feedback, and explicit global promotion.

## Core principle

Conduit memory is not a transcript archive and not a generic repository RAG database. It is a curated, evidence-backed knowledge layer containing compact information that prevents agents from repeatedly rediscovering:

- architectural decisions;
- contracts and invariants;
- project conventions;
- ownership boundaries;
- reliable commands;
- recurring pitfalls;
- verified corrections;
- predecessor-agent outcomes;
- reusable global lessons.

Raw source code remains in the repository. Raw transcripts, command output, private reasoning, and unredacted logs remain outside the memory retrieval corpus.

## Non-goals

- No direct database access by spawned agents.
- No automatic promotion of project knowledge to global memory.
- No embedding of full source files, raw transcripts, raw prompts, raw logs, credentials, or unvalidated handoff content.
- No mandatory vector database, remote embedding service, automatic LLM summarization, or cheap-model refresh in the initial implementation.
- No duplication of Feature 002 database bootstrap or migration infrastructure.
- No use of stale, invalid, unauthorized, or contradictory memory in context packs.

## Memory scopes

- `run`: ephemeral observations and handoffs for one run.
- `feature`: knowledge valid for a feature packet or task group.
- `project`: durable project-local memory in `<project>/.conduit/state.db`.
- `global`: explicitly promoted reusable user-global lessons in the user-global database.

Global memory is opt-in through explicit promotion. Never automatically promote source code, repository paths, customer/company identifiers, credentials, raw prompts, raw transcripts, unredacted logs, or feature-specific implementation details. Global memories contain generalized reusable lessons, conventions, or preferences with provenance pointing to the project memory that produced them.

## Logical addressing and memory kinds

Memory uses filesystem-like logical addresses, for example:

```text
project://<fingerprint>/architecture/auth
project://<fingerprint>/contracts/api/orders
project://<fingerprint>/commands/test/backend
feature://003/decisions/storage-owner
run://<run-id>/handoffs/backend
global://typescript/node-next/import-rules
```

Memory kinds include:

- `fact`;
- `decision`;
- `contract`;
- `convention`;
- `command`;
- `pitfall`;
- `observation`;
- `handoff`;
- `promoted-lesson`.

Each durable memory version stores stable identity/logical path, scope, kind, compact summary, optional detailed body, confidence, lifecycle state, source provenance, evidence hashes, creator role/run, creation/update timestamps, version, optional expiry, estimated prompt-token cost, security classification, and optional embedding metadata.

## Memory lifecycle

```text
observation proposed
        ↓
validated and redacted
        ↓
deduplicated/merged
        ↓
evidence attached
        ↓
embedding generated when enabled
        ↓
active
        ↓
stale / superseded / expired / rejected / tombstoned
```

### Proposal

Agents cannot create active memories directly. An agent handoff may include `suggestedMemories`; each proposal includes proposed kind, concise summary, optional detail, evidence references, confidence rationale, intended scope, and suggested logical path.

Deterministic Conduit components may also propose observations based on approved feature contracts, configuration, repository metadata, successful verification commands, explicit architect/user decisions, and validated predecessor handoffs. Conduit must not turn every file, command, or agent statement into memory.

### Validation

Before activation, Conduit validates the handoff/proposal schema, enforces size limits, redacts secrets and sensitive values, confirms the proposed scope is permitted, confirms referenced evidence exists, captures current evidence hashes, rejects unsupported or unverifiable claims, detects duplicates and contradictions, and decides whether to create, merge, supersede, or reject the proposal.

### Activation

An active memory has provenance explaining where it came from, which evidence supports it, which task/role created it, when it was last validated, which memory it supersedes if applicable, and which embedding provider/model/version produced its vector if embeddings are enabled.

### Correction and supersession

Incorrect memories are not silently deleted. Lifecycle states are `active`, `stale`, `superseded`, `expired`, `rejected`, and `tombstoned`. A correction creates a new version and links to the superseded memory. Historical records remain available for audit but are excluded from context packs.

### Invalidation

A memory becomes stale when required evidence content hashes change, an approved contract supersedes it, relevant configuration changes, expiry is reached, embedding representation/model becomes incompatible with configuration, or an architect/user explicitly invalidates it. Stale memories are excluded before lexical or vector ranking. Similarity never resurrects invalid memory.

## Repository index versus memory corpus

### Repository index

The repository index tracks paths, file kinds, content hashes, ownership, deterministic symbols/headings where available, feature/contract associations, ignored/generated/binary status, and oversized/secret exclusions. Its purpose is change detection, evidence resolution, deterministic lookup, and stale-memory detection.

Indexing is deterministic and incremental. It excludes ignored files, `.git`, `.conduit`, dependencies/vendor directories, generated build output, binaries, oversized files, secrets, raw transcripts, and agent logs unless transformed into bounded redacted observations. Source content hashes decide evidence validity.

### Memory corpus

The memory corpus stores curated observations and decisions. The initial implementation embeds only active, validated memory records when embeddings are enabled. It must not embed the complete repository. A future code/document semantic index is a separate feature and table set and must not silently expand agent memory scope.

## Optional Turso vector support

Use Turso native vector capabilities as an optional semantic retrieval layer in embedded project and global databases. Turso vector support includes vector BLOB values, `vector32(...)`, quantized `vector8(...)`, `vector_distance_cos(...)`, and related distance functions. For a small project-memory corpus, begin with filtered linear cosine-distance queries. Approximate vector indexes are not required initially.

Do not assume Turso Cloud/libSQL DiskANN index syntax is supported identically by the selected embedded `@tursodatabase/database` version. Any vector-index capability requires a compatibility spike and benchmark before becoming required.

## Embedding provider abstraction

Define an `EmbeddingProvider` port in the memory domain or an appropriate infrastructure boundary with concepts equivalent to:

```ts
interface EmbeddingProvider {
  readonly id: string;
  readonly model: string;
  readonly dimensions: number;
  readonly representation: "float32" | "float8";

  embedDocuments(inputs: string[]): Promise<number[][]>;
  embedQuery(input: string): Promise<number[]>;
}
```

Requirements:

- Embeddings are optional; deterministic and lexical retrieval must work without a provider.
- No remote embedding API is enabled by default.
- A local embedding provider is preferred for local-first implementation.
- Remote providers may be adapters later with explicit credentials, cost, and privacy configuration.
- Embedding-provider credentials never enter either database.
- Provider/model identity, dimensions, representation, canonical-document hash, vector value, generation timestamp, and failure/retry metadata persist with every embedding.
- Changing the model, dimensions, or representation schedules controlled re-embedding and must not interpret old vectors with the new model.
- A 384-dimensional local sentence embedding with `F8_BLOB(384)` may be evaluated, but benchmark results must drive the final choice.

The embedding benchmark must compare retrieval relevance, latency, database size, standalone executable impact, CPU/memory consumption, Float32 versus Float8 recall, cold-start behavior, and batch generation performance.

## Canonical retrieval document

Embeddings are generated from a canonical document built from memory metadata:

```text
kind: <kind>
path: <logical path>
title: <short title>
summary: <compact verified summary>
keywords: <normalized keywords>
```

Do not embed raw transcripts, chain-of-thought/private reasoning, complete source files, secrets, credentials, raw environment variables, complete logs, unvalidated handoff content, or stale/rejected observations. Store a canonical-document hash and regenerate only when the hash changes, embedding model changes, representation changes, or the previous embedding failed/missing. Embedding work is batched and may run after the relational memory transaction. A failed embedding must not prevent storing a valid lexical memory.

## Hybrid retrieval pipeline

### Phase 1: Query construction

Build a retrieval query from feature/task-group description, assigned role, owned paths, applicable contracts, predecessor handoff, and explicit user/architect requests. Generate structured filters, normalized lexical terms, and an optional query embedding.

### Phase 2: Mandatory deterministic context

Select mandatory items before relevance search: assigned acceptance criteria, applicable contracts, role instructions, ownership/forbidden paths, required verification commands, and explicit architect decisions. Mandatory context cannot be displaced by vector-similar optional memories.

### Phase 3: Eligibility filtering

Filter candidates by active lifecycle state, scope, project fingerprint, feature/run relationship, role visibility, ownership/path relevance, evidence validity, expiry, and security classification. Global memory is searched separately and only among approved promoted records.

### Phase 4: Candidate generation

Generate candidates through exact logical-path matches, contract/feature relationships, normalized lexical matches, predecessor/dependency links, vector cosine similarity when embeddings are available, and recent high-confidence memories for the same role and owned paths. Vector search retrieves more candidates than the final count so rank fusion can discard semantically similar but structurally irrelevant results.

### Phase 5: Rank fusion

Combine deterministic, lexical, and vector results using documented weighted scoring or reciprocal-rank fusion. Exact applicable contracts outrank semantic similarity; direct valid evidence outranks indirect evidence; role/path relevance is a strong boost; project memory normally outranks global memory; high confidence increases rank; stale/invalid/unauthorized records are ineligible; similarity alone cannot establish truth; retrieval count alone cannot establish usefulness; recency is modest. Store score explanations.

### Phase 6: Deduplication and diversity

Collapse duplicates and superseded memories, prefer newest valid versions, avoid filling packs with equivalent observations, preserve diversity across contracts/decisions/pitfalls/commands, and surface contradictions as blockers instead of silently selecting one.

### Phase 7: Token-budget assembly

Assemble context packs in this order:

1. assignment and acceptance criteria;
2. mandatory contracts;
3. ownership/security restrictions;
4. predecessor handoffs;
5. verification commands;
6. project memories;
7. global memories;
8. optional supporting details.

Each selected item records estimated token cost, priority, selection reason, source memory ID, evidence reference, and truncation policy. Do not truncate mid-claim; use compact summary or omit optional detail. Vector search improves candidate selection; hard context-pack budgets produce token savings.

Suggested configurable defaults:

- ordinary worker: about 1,500 tokens;
- integrator/QA: about 2,500 tokens;
- architect/reviewer: about 4,000 tokens;
- individual handoff: about 600-800 tokens maximum.

## Context packs

Before launching an agent, Conduit creates a prompt-ready context pack containing only the assignment needs: task group, acceptance criteria, contracts, role guidance, owned/forbidden paths, relevant project observations, predecessor handoffs, blockers, and verification commands.

Context packs must have a configurable hard token/character budget, prioritize mandatory contracts before optional observations, avoid invalid/misleading fragments, record why each item was selected, identify omitted material, cache by task/role/configuration/repository-index revision, invalidate when relevant evidence/configuration/task contracts change, and expose estimated and actual prompt size for observability.

## Structured handoffs

Agents return bounded structured handoff envelopes rather than transcript dumps. The versioned contract includes schema version, run, feature, task group, role, concise outcome summary, changed files, contracts created/consumed, decisions made, blockers/unresolved questions, commands/tests executed, failures, risks, evidence references, suggested memories, suggested global promotions, and completion status.

Conduit validates, size-checks, redacts, and stores envelopes. Agents cannot write memories directly. Suggested memories are proposals. Conduit decides whether they become active project memories. Global promotion requires explicit user or architect approval.

## Retrieval attribution and feedback

For every memory included in a context pack, create a retrieval-attribution record containing task/run ID, memory ID and version, retrieval channel, lexical/vector scores, final rank, selection reason, estimated tokens, whether the agent cited or used it, later usefulness feedback, and task outcome metadata.

Feedback does not automatically rewrite memory truth. It may affect ranking weight only after sufficient evidence and remains bounded. Explicit corrections and evidence validity take precedence. Track verification pass/fail, agent contradiction, architect acceptance/rejection, avoided repository exploration, caused rework, and tokens included versus referenced.

## Global memory promotion

Promotion creates a new generalized record in the user-global database. It is not a direct row copy. Promotion requires explicit approval, removes project-specific paths/identifiers, generalizes the lesson, preserves audit provenance through project fingerprint and source memory ID, reruns redaction, generates a new canonical retrieval document, generates a separate global embedding when enabled, prevents later project deletion from exposing copied raw evidence, and allows revocation/supersession.

Example project memory:

```text
project://abc/commands/test/backend
Run `pnpm --filter conduit-orchestrator test` for backend changes.
```

Potential global lesson:

```text
global://preferences/javascript/package-scoped-tests
Prefer package-scoped tests before full-monorepo verification when the project exposes package filters.
```

The exact project command remains project-local.

## Suggested relational model

The schema may use tables equivalent to:

- `memory_nodes`;
- `memory_versions`;
- `memory_evidence`;
- `memory_embeddings`;
- `memory_relations`;
- `memory_proposals`;
- `memory_retrievals`;
- `memory_feedback`;
- `memory_promotions`;
- `context_packs`;
- `context_pack_items`;
- `source_versions`.

A simpler normalized model is acceptable if it satisfies lifecycle, evidence, attribution, audit, and retrieval requirements. Embedding rows include memory ID/version, provider ID, model ID, dimensions, representation, canonical-document hash, vector value, generation timestamp, and failure/retry metadata.

## Acceptance criteria

- [ ] Repository indexing is deterministic, incremental, excludes ignored/binary/generated/secret/transcript/log material, and tracks content hashes.
- [ ] Memory proposals pass through validation, redaction, deduplication/merge, evidence attachment, optional embedding, and lifecycle activation.
- [ ] Active memory is evidence-backed and becomes stale immediately when required evidence changes.
- [ ] Stale, superseded, expired, rejected, tombstoned, unauthorized, or invalid-evidence memory is excluded before lexical/vector ranking.
- [ ] Retrieval works deterministically without embeddings and uses optional Turso vector search only as one candidate-generation channel.
- [ ] Hybrid retrieval records score explanations and attribution for included memories.
- [ ] Context packs enforce hard budgets, prioritize mandatory contracts, never truncate mid-claim, identify omissions, and cache/invalidate correctly.
- [ ] Handoff envelopes are versioned, validated, redacted, bounded, and stored without transcript dumps.
- [ ] Suggested memories and global promotions are proposals; only Conduit activates project memory and explicit approval creates global memory.
- [ ] Global memory is generalized, opt-in, provenance-backed, revocable/supersedable, and stored in the global database without raw project evidence.
- [ ] No spawned agent receives database access, raw project history, raw transcripts, credentials, or unbounded memory.
- [ ] Token-efficiency tests distinguish measured prompt sizes from illustrative estimates and show reduced repeated context across representative launches.

## Feature 007 Alignment

Feature 007 supersedes any Feature 003 wording that implies a separate handoff wire format. Memory handoff data is carried as `memoryProposals` and `globalPromotionProposals` inside the universal `AgentResponseV1`, then persisted with Conduit-owned metadata in `ConduitResultRecordV1`. Agents still cannot activate memory or write project/global databases directly.
