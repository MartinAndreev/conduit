# Feature 007 tasks

## Completed: universal V1 communication protocol

- [x] Create the protocol feature packet and universal JSON Schemas.
- [x] Add strict TypeScript protocol types and structural validation.
- [x] Add assignment semantic validation policies for built-in role classes.
- [x] Add incremental JSONL/native event parsing through the system runner boundary.
- [x] Use the system runner registry from orchestration.
- [x] Require a final validated response for completion and dependency unlocking.
- [x] Convert final review to structured verdict/findings.
- [x] Render research Markdown from validated universal responses.
- [x] Include bounded project-memory and global-promotion proposals without granting activation or database access.
- [x] Add protocol, parser, semantic-policy, security, and completion coverage.

## Approved design scope, implementation not started: long-term memory

### 007-M0 contracts, configuration, and compatibility

- [ ] Add provider-neutral memory ports/DTOs and the exact-selection factory/registry.
- [ ] Extend global/project/effective configuration and provenance without environment or implicit CLI overrides.
- [ ] Keep memory disabled in shipped defaults until every M0-M5 release gate passes.
- [ ] Freeze and verify the exact Memvid SDK/runtime/data-format version.
- [ ] Pass the Memvid compatibility spike and all four standalone target gates before default shipping.
- [ ] Freeze and verify the optional Hindsight HTTP/API version and candidate-only behavior.

### 007-M1 canonical lifecycle and evidence

- [ ] Add canonical project/global migrations and repositories.
- [ ] Snapshot accepted responses/proposals immutably before the latest result read-model upsert.
- [ ] Implement idempotent proposal validation, redaction, eligibility, deduplication, contradiction, activation, rejection, supersession, expiry, staleness, and tombstone rules.
- [ ] Implement typed evidence and commit/contract/verification/promotion validity.
- [ ] Keep canonical truth status separate from provider projection and embedding/representation status.
- [ ] Keep unsupported evidence-optional proposals pending and add explicit decisions for ambiguous cases.

### 007-M2 projection providers and outbox

- [ ] Add transactional projection outbox, stable operation IDs, retries, watermarks, deletion acknowledgements, and verified rebuild generations.
- [ ] Implement Memvid as the required default rebuildable projection/search provider.
- [ ] Implement Hindsight as an explicitly configured daemon/HTTP alternative.
- [ ] Implement explicitly configured, observable Turso lexical degraded mode.
- [ ] Add bounded retry classes, attempt/elapsed caps, backoff/jitter, terminal ledger state, and launch-time synchronization deadlines.
- [ ] Pass common provider conformance and cross-project isolation tests.

### 007-M3 retrieval and context delivery

- [ ] Implement launch-time snapshots, bounded candidate requests, canonical hydration/eligibility, deterministic ranking, attribution, contradiction handling, and omission records.
- [ ] Enforce the hard complete Conduit-authored context budget, output reserve, and mandatory-overflow failure.
- [ ] Materialize immutable transient pack files, append exact references to unchanged `AgentAssignmentV1.contextReferences`, and enforce write protection through `forbiddenPaths` and execution policy.
- [ ] Compile a fresh snapshot for every initial, retry, correction, re-review, and resumed assignment after dependencies settle.
- [ ] Exclude and clean registered transient files without exposing sibling or database state.

### 007-M4 cache, deletion, and global promotion

- [ ] Implement generation-complete cache keys and invalidation.
- [ ] Implement inspect/explain/invalidate/delete flows that cannot resurrect stale or tombstoned memory.
- [ ] Implement explicit global-promotion approval, generalization, provenance, revocation, and provider deletion.

### 007-M5 evaluation and release

- [ ] Run controlled no-memory, Turso lexical degradation, and Memvid evaluation with equal total prompt budgets.
- [ ] Prove security, isolation, budget, restart, deletion, and deterministic failure gates.
- [ ] Pass full package checks and native memory-provider standalone conformance on Linux x64, Linux ARM64, macOS ARM64, and Windows x64.
- [ ] Enable the post-qualification Memvid target default only after every release gate passes.

Unchecked memory tasks are approved design scope only. They do not authorize implementing a later group before its assignment.
