# Tasks

## 003-A Contracts, schema, and lifecycle foundation

- [ ] Define memory scopes, memory kinds, lifecycle states, confidence, provenance, evidence reference, and prompt-token-cost types in the memory domain.
- [ ] Define versioned handoff envelope, memory proposal, context-pack, context-pack item, retrieval attribution, feedback, and promotion contracts.
- [ ] Add project/global migrations for memory lifecycle tables through the Feature 002 migration registry.
- [ ] Implement proposal validation, size limits, redaction, permitted-scope checks, evidence existence checks, duplicate detection, contradiction detection, and create/merge/supersede/reject decisions.
- [ ] Ensure agents cannot create active memories directly.

## 003-B Deterministic repository index and evidence invalidation

- [ ] Implement deterministic incremental repository indexer.
- [ ] Exclude ignored files, `.git`, `.conduit`, dependencies, vendor directories, generated output, binaries, oversized files, secrets, raw transcripts, and agent logs unless transformed into bounded redacted observations.
- [ ] Track path, file kind, content hash, ownership, deterministic headings/symbols when available, feature/contract association, and exclusion reason.
- [ ] Attach evidence hashes to memory versions.
- [ ] Mark memory stale when required evidence changes, contracts/configuration supersede it, expiry is reached, embedding compatibility changes, or architect/user invalidates it.

## 003-C Deterministic and lexical retrieval

- [ ] Build retrieval query construction from assignment, role, owned paths, contracts, predecessor handoffs, and explicit requests.
- [ ] Select mandatory deterministic context before optional search.
- [ ] Implement eligibility filtering by lifecycle state, scope, project fingerprint, feature/run relationship, role visibility, path relevance, evidence validity, expiry, and security classification.
- [ ] Implement exact-path, contract/feature, lexical, predecessor/dependency, and recent-high-confidence candidate channels.
- [ ] Implement rank fusion with score explanations, deduplication, diversity, and contradiction blocker detection.
- [ ] Persist retrieval attribution for every selected memory.

## 003-D Optional embeddings and Turso vector retrieval

- [ ] Define `EmbeddingProvider` interface in the correct memory/infrastructure boundary.
- [ ] Implement canonical retrieval document generation and hash storage.
- [ ] Implement optional batched embedding generation for active validated memories only.
- [ ] Store provider ID, model ID, dimensions, representation, canonical hash, vector value, generation timestamp, and failure/retry metadata.
- [ ] Implement filtered linear cosine vector candidate generation using Turso vector capabilities when configured.
- [ ] Reject dimension/model/representation mismatches and schedule controlled re-embedding.
- [ ] Add labelled evaluation corpus and benchmark Float32 versus Float8 plus relevance, latency, DB size, standalone impact, CPU/memory, cold start, and batch performance.

## 003-E Context-pack compiler and cache

- [ ] Implement hard configurable token/character budgets with defaults for worker, QA/integrator, architect/reviewer, and individual handoff sizes.
- [ ] Assemble context packs in required priority order: assignment, contracts, restrictions, predecessor handoffs, verification commands, project memory, global memory, optional detail.
- [ ] Prevent mid-claim truncation by using compact summaries or omitting optional detail.
- [ ] Record selected and omitted items with reasons, priorities, estimated token costs, source memory IDs, evidence references, and truncation policies.
- [ ] Cache context packs by task, role, configuration, repository-index revision, evidence hashes, contracts, and relevant memory/handoff versions.
- [ ] Invalidate cache when relevant evidence, configuration, contracts, role guidance, memory, or handoff changes.
- [ ] Update run planning so agents receive only context-pack files/prompts and no database access.

## 003-F Structured handoffs and memory proposal ingestion

- [ ] Validate versioned handoff envelopes from agent process output.
- [ ] Enforce handoff size limits and reject transcript dumps, malformed envelopes, unredacted secrets, unauthorized paths, and missing required fields.
- [ ] Store accepted handoffs as bounded envelopes with run, feature, task group, role, outcome, changed files, consumed/created contracts, decisions, blockers, commands/tests, failures, risks, evidence references, suggestions, and completion status.
- [ ] Convert `suggestedMemories` and `suggestedGlobalPromotions` into proposals only after validation and redaction.
- [ ] Make predecessor handoffs available to retrieval/context-pack compilation under budget.

## 003-G Global promotion, feedback, and token-efficiency evaluation

- [ ] Add explicit user/architect approval command for global promotion.
- [ ] Generalize approved project memory into a new global record without project paths, identifiers, raw evidence, credentials, or feature-specific details.
- [ ] Preserve audit provenance with project fingerprint and source memory ID/version.
- [ ] Generate separate global canonical document and embedding when enabled.
- [ ] Allow global memory revocation and supersession.
- [ ] Persist retrieval feedback and task outcome metadata without automatically rewriting memory truth.
- [ ] Add token-efficiency tests that compare measured prompt sizes and repeated-context reduction across representative launches.
