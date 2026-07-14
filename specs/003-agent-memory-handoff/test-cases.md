# Test cases

## Repository index and evidence

- [ ] Indexing is deterministic across repeated runs with unchanged input.
- [ ] Ignored files, `.git`, `.conduit`, dependency/vendor directories, generated output, binaries, oversized files, secrets, raw transcripts, and unredacted logs are excluded with recorded reasons.
- [ ] Source content hashes are stable and change when file contents change.
- [ ] Memory with changed required evidence becomes stale immediately.
- [ ] Stale memory is excluded before lexical or vector ranking.
- [ ] Project fingerprints prevent memory from one project appearing in another project's retrieval.

## Memory lifecycle and security

- [ ] Agent `suggestedMemories` create proposals, not active memory.
- [ ] Valid proposals are redacted, deduplicated/merged, evidence-attached, and activated.
- [ ] Unsupported or unverifiable claims are rejected with sanitized diagnostics.
- [ ] Corrections create new versions and supersede older versions without silent deletion.
- [ ] Expired, rejected, superseded, tombstoned, unauthorized, and stale memories are excluded from context packs.
- [ ] Seeded secrets never appear in memory summaries, bodies, canonical embedding documents, context packs, handoffs, retrieval attribution, or global promotions.

## Retrieval without embeddings

- [ ] Retrieval works with embeddings disabled through deterministic and lexical channels.
- [ ] Role/path ownership filtering excludes irrelevant or forbidden memories.
- [ ] Exact applicable contracts outrank semantically similar observations.
- [ ] Retrieval results are deterministic when scores tie.
- [ ] Contradictory candidate memories surface a blocker rather than silently selecting one.
- [ ] Retrieval attribution persists channel, scores, rank, reason, estimated tokens, memory ID/version, and task/run IDs.

## Vector and hybrid retrieval

- [ ] Cosine retrieval returns semantically related active memories from the labelled evaluation corpus.
- [ ] Hybrid retrieval outperforms vector-only retrieval on contract/path queries using expected top-k membership or precision/recall.
- [ ] Invalid evidence excludes a high-similarity memory before vector ranking.
- [ ] Project isolation is applied before vector search.
- [ ] Global memory is searched only among explicitly promoted records.
- [ ] Dimension mismatch is rejected and schedules controlled re-embedding.
- [ ] Embedding-model or representation change schedules controlled re-embedding and does not reinterpret old vectors.
- [ ] Canonical-document hash prevents unnecessary re-embedding.
- [ ] Float32 versus Float8 benchmark reports relevance and storage differences.
- [ ] Embedding failure records retry metadata and does not lose relational memory.
- [ ] No raw transcript, source-code, secret, credential, environment-variable, or complete-log content is embedded.
- [ ] Representative corpus tests report acceptable latency and database growth rather than only proving a query executes.

## Context packs

- [ ] Context packs enforce hard token/character budgets.
- [ ] Mandatory contracts and acceptance criteria are included before optional observations.
- [ ] Optional details are omitted or summarized rather than truncated mid-claim.
- [ ] Selected items record reasons, priority, estimated token cost, source memory ID, and evidence reference.
- [ ] Omitted material is identified with omission reasons.
- [ ] Cache hits occur for identical task/role/config/index/evidence/contract inputs.
- [ ] Cache invalidates when relevant evidence, configuration, task contracts, role guidance, handoffs, or memory versions change.
- [ ] Measured prompt sizes are captured separately from illustrative estimates.
- [ ] Representative multi-agent launches show reduced repeated context compared with full packet/history prompts.

## Handoffs

- [ ] Well-formed bounded handoff envelopes are accepted and stored.
- [ ] Malformed handoffs are rejected.
- [ ] Oversized handoffs are rejected.
- [ ] Transcript dumps are rejected.
- [ ] Handoffs with unredacted secrets are rejected or redacted before storage according to policy.
- [ ] Suggested global promotions remain proposals until explicitly approved.
- [ ] Agents cannot write memories directly and cannot access either database.

## Global promotion

- [ ] Project memory is not promoted automatically.
- [ ] Explicit approval creates a generalized global memory record, not a row copy.
- [ ] Promotion strips project paths, identifiers, raw evidence, and feature-specific implementation details.
- [ ] Global memory provenance includes project fingerprint and source memory ID/version.
- [ ] Global embeddings are generated separately from project embeddings when enabled.
- [ ] Revoked or superseded global memory is excluded from future context packs.

## Labelled evaluation corpus

The vector/hybrid test suite must include representative labelled memories for architecture decisions, API contracts, test commands, role ownership rules, pitfalls, corrections, semantically similar but inapplicable memories, stale memories, and global reusable lessons. Success is measured with expected top-k membership, recall/precision, and latency/database-size thresholds.
