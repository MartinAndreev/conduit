# Feature 007 test cases

## Implemented V1 protocol

- [x] Incremental JSONL parsing handles records split across arbitrary chunks.
- [x] Extra prose, Markdown fences, malformed/multiple JSON, unknown properties, oversize values, invalid enums/paths, and seeded secrets fail structural validation.
- [x] Assignment semantic policy can accept the same structure for one role kind and reject it for another.
- [x] Completion requires the role-appropriate artifacts, verification, verdict, findings, blockers, or questions.
- [x] Exit zero with invalid, partial, blocked, or `needs_input` output does not unlock dependents.
- [x] Research Markdown is rendered from a validated universal response.
- [x] Agents receive no database environment variables.

## Proposed long-term memory contract and compatibility

- [ ] The three checked-in V1 schemas are byte-for-byte identical before and after memory implementation.
- [ ] No runtime path creates or accepts a memory-specific handoff envelope.
- [ ] Communication providers and run/TUI code have no imports of Memvid or Hindsight adapters.
- [ ] The factory creates exactly the effective configured provider and rejects unknown/disabled IDs.
- [ ] A Memvid failure never probes or creates Hindsight; a Hindsight failure never opens Memvid.
- [ ] Configuration resolves each memory field as project `conduit.yml` over global settings over built-in default and records provenance.
- [ ] Role global profiles and environment variables do not override memory configuration.
- [ ] Raw credential values in project/global configuration are rejected; opaque credential-profile/secret references resolve only at adapter construction.
- [ ] Partial M0-M5 implementations keep shipped memory disabled and do not change ordinary role launches.
- [ ] After every release gate passes, target defaults enable memory with `memvid` and `degradedMode: disabled`.
- [ ] Unknown provider SDK/runtime/data-format/API versions fail capability negotiation and never enter lexical degraded mode.
- [ ] Capability descriptors report supported/unsupported/unknown accurately and enforce documented bounds.
- [ ] Effective provider limits are the minimum of configuration, Conduit hard caps, and provider descriptor caps.
- [ ] Identifier/namespace/path/summary/keyword/document limits and the 8-query/8,192-query-byte/200-candidate/65,536-response-byte/256-document/2-MiB-rebuild/4,096-diagnostic-byte caps reject oversized data exactly as specified.

## Memvid default qualification

- [ ] A frozen exact Memvid SDK/runtime/data-format version passes NodeNext import and Bun package/build tests.
- [ ] Memvid round-trips exact namespace, memory ID, version, document hash, provider revision, and watermark.
- [ ] Duplicate upserts/deletes are idempotent and do not duplicate or boost candidates.
- [ ] Interrupted write/checkpoint/rebuild leaves either the prior verified generation or a new fully verified generation.
- [ ] Concurrent process/lock behavior fails deterministically without corrupting the projection.
- [ ] Exact delete or verified rebuild removes a tombstoned item from search.
- [ ] Lexical search is bounded and deterministic after Conduit tie-breaking.
- [ ] Optional vector/ONNX behavior stays disabled unless separately frozen and qualified.
- [ ] The same frozen dependency set persists and retrieves without `node_modules` in `linux-x64`, `linux-arm64`, `darwin-arm64`, and `windows-x64` standalone artifacts.

## Hindsight alternative qualification

- [ ] Hindsight starts only when effective configuration selects it.
- [ ] External HTTP and any approved managed-daemon lifecycle pass bounded startup/health/timeout/cancellation/shutdown tests.
- [ ] Managed lifecycle makes no persistent change to Hindsight, project, global, or harness configuration.
- [ ] Requests use the project namespace/bank and reject cross-project results.
- [ ] Recall results are capped by count, bytes, deadline, and allowed candidate identity.
- [ ] Unknown/duplicate/stale IDs and wrong versions are discarded and reported as bounded diagnostics.
- [ ] Reflect/synthesized answers never become canonical content or context-pack text.
- [ ] HTTP errors, bodies, diagnostics, and process arguments contain no credentials or canonical document text.

## Immutable ingestion and lifecycle

- [ ] The same accepted response delivered repeatedly creates one immutable response snapshot and one proposal per ordinal.
- [ ] A retry/correction with a new assignment ID creates distinct provenance even when content is identical.
- [ ] Ingesting a later retry and updating `result_records` by run/role does not change the earlier proposal's assignment ID, response hash, ordinal, evidence, or timestamps.
- [ ] A response failing structural, security, path, size, or assignment-semantic validation creates no snapshot or proposal.
- [ ] The proposal identity is a SHA-256 length-delimited repository/run/assignment/response-hash/ordinal tuple and cannot collide through embedded delimiters.
- [ ] Semantically accepted partial/failed/blocked results may create pending proposals under failure-lesson policy but never unlock dependents.
- [ ] A failure lesson activates only with independent observed failure evidence and remains explicitly failure-scoped.
- [ ] A V1 proposal without evidence remains pending and does not autoactivate.
- [ ] Agent-authored evidence strings alone do not satisfy independent evidence.
- [ ] An unambiguous proposal fully supported by current Conduit-observed evidence follows the deterministic project activation policy.
- [ ] Ambiguous, contradictory, sensitive, external, or unsupported proposals remain pending or are rejected with sanitized reasons.
- [ ] A local/authenticated human command/UI action or separately human-authorized Conduit policy can decide an ambiguous proposal; authorization identity and scope are audited.
- [ ] Architect/reviewer agent responses and verdicts remain proposals and cannot issue or impersonate approval, activation, rejection, supersession, invalidation, tombstone, or promotion decisions.
- [ ] Correction creates a new active version and supersedes the prior version without deleting audit history.
- [ ] Truth, projection, and embedding/representation states change independently and are observable; failed/incompatible embeddings do not alter truth or participate in search.

## Evidence and commit validity

- [ ] Current-project claims require an approved promoted commit/current matching content, not an agent artifact claim.
- [ ] Contract claims become stale when approved package/path/content hashes change without an explicit compatibility relation.
- [ ] Source evidence validates repository ID, commit object, relative path, blob/content hash, and current applicability.
- [ ] Verification evidence links to persisted Conduit observation/process identity and does not trust response text alone.
- [ ] Worktree cleanup preserves validity when commit/blob/hashes/promotion/repository identity remain verifiable.
- [ ] Missing Git objects, changed content, superseding contract, revoked decision, expiry, or repository mismatch makes dependent memory stale before search.
- [ ] An unpromoted failed branch may support a bounded failure lesson but cannot support a current-success claim.
- [ ] Two repositories with the same basename never share evidence, provider namespace, cache, or memory.

## Outbox, projection, deletion, and rebuild

- [ ] Activation/lifecycle change and its outbox operation commit atomically or both roll back.
- [ ] Retrying the same outbox operation preserves one stable operation ID and logical effect.
- [ ] A delayed/retried upsert with an older canonical truth sequence cannot overwrite a later stale/tombstone delete.
- [ ] Truth sequence is an unsigned 64-bit decimal string and watermark comparison succeeds only for equal namespace/configuration/projection generations using integer comparison.
- [ ] Applying sequence N+1 for memory B before sequence N for memory A does not advance the applied watermark through N+1 or cover snapshot N+1 until N is applied or proven obsolete by a newer canonical operation for A.
- [ ] Missing, pending, retryable-failed, and terminal-failed operations block the continuous applied watermark prefix.
- [ ] Search does not use a projection behind the snapshot's required watermark.
- [ ] With degradation disabled, unavailable/lagging selected provider blocks launch before an agent receives context.
- [ ] With `degradedMode: turso-lexical` explicitly effective, only transient availability/query/I/O/watermark failure from a compatible provider uses canonical Turso lexical candidates.
- [ ] Unknown/incompatible version, contract/schema/capability/configuration/authentication/secret-policy, security, namespace, evidence, budget, and canonical database failures never enter degraded mode.
- [ ] Only documented transient failure classes retry; terminal classes write failed ledger state immediately.
- [ ] Retry stops after eight attempts or fifteen minutes, uses 250-ms-to-30-s capped exponential backoff with full jitter, and requires explicit action after exhaustion.
- [ ] Launch waits only to a bounded synchronization deadline; an unsynced watermark degrades only for an eligible transient cause or blocks with pending/terminal ledger state.
- [ ] Provider deletion lag cannot make stale/tombstoned truth eligible.
- [ ] Tombstones remain until projection/cache acknowledgement or verified rebuild.
- [ ] A provider returning deleted or stale versions cannot resurrect them.
- [ ] Interrupted rebuild remains inactive; a rebuild catches up from its source watermark through every later tombstone before verified count/hash/sample checks and atomic generation switch.

## Retrieval and context snapshots

- [ ] Context compilation occurs after dependencies and eligible proposal activation and immediately before each role launch.
- [ ] Initial, retry, correction, re-review, and resumed assignments each receive a new immutable pack keyed to their assignment ID.
- [ ] A running role's pack never changes when another role produces or activates memory.
- [ ] The next dependency launch can see an eligible memory activated before its snapshot.
- [ ] Provider query count/total bytes, individual query bytes, result/unique-candidate count, response bytes, deadline, namespace, generation, and watermark enforce the normative hard caps.
- [ ] Every candidate is reloaded from Turso at the same snapshot and rechecked for identity, version, scope, lifecycle, evidence, security, and expiry.
- [ ] Exact current contracts/decisions outrank provider similarity.
- [ ] Deterministic ranking breaks equal scores stably, deduplicates equivalent claims, and preserves useful kind diversity.
- [ ] Contradictory eligible claims cause a blocker/omission diagnostic instead of arbitrary selection.
- [ ] Retrieval attribution records request, provider/degraded channel, scores/rank, selection/omission reason, memory ID/version, evidence generation, token cost, and pack/assignment identity.
- [ ] Provider ID/revision, scores, capability detail, degraded reason, and omission reasons remain only in canonical audit metadata.

## Hard budget and delivery

- [ ] Budget accounting includes serialized assignment, role/system guidance, response instructions, feature packet/contracts, dependency/retry feedback, memory, labels, boilerplate, separators, and reserved output.
- [ ] Verified tokenizer counts are used when available; conservative fallback counts and method are recorded.
- [ ] Mandatory overflow blocks before provider/agent launch and identifies bounded contributors.
- [ ] Optional memory is omitted as a whole claim and no item is cut mid-claim.
- [ ] Actual serialized size is at or below the effective hard budget in every successful case.
- [ ] The pack is persisted immutably and materialized read-only at a registered `.conduit-context/<assignment-hash>/context-pack-v1.md`-equivalent path.
- [ ] The exact relative path appears in unchanged `AgentAssignmentV1.contextReferences`, appears in `forbiddenPaths` for mutation protection, is read-authorized by execution policy, and its file hash matches the pack record.
- [ ] Delivered Markdown contains canonical mandatory/memory content plus only provider-neutral degraded/omitted notices; it contains no provider ID/revision/score/capability/failure reason.
- [ ] The transient path is visible only in its role workspace, excluded from Git changed-file ownership/integration, and removed after completion or bounded crash cleanup.
- [ ] Agents receive no database path, provider configuration/credentials, outbox, raw provider response, or sibling pack.

## Cache, global promotion, and security

- [ ] Identical full cache identities hit; any relevant assignment, HEAD, package/contract, dependency, truth/evidence, global approval, provider/configuration, watermark, budget/tokenizer, or compiler change misses.
- [ ] Cache lookup revalidates project identity, truth/evidence generations, degradation state, and content hash.
- [ ] Stale, superseded, expired, rejected, tombstoned, unauthorized, wrong-project, or invalid-evidence memory never appears from cache or provider.
- [ ] Project memory never enters the global database without a separate explicit promotion approval.
- [ ] Global approval is audited to a local/authenticated human command/UI action or a separately human-authorized policy; an architect-role response cannot approve its own proposal.
- [ ] Promotion creates generalized global content, removes project identifiers/raw evidence, preserves source memory/version and approval provenance, and uses separate lifecycle/projection records.
- [ ] Global revocation/supersession/tombstone removes future eligibility and propagates provider/cache deletion.
- [ ] Seeded secrets and raw transcripts appear in none of canonical rows, provider documents, caches, packs, diagnostics, rendered reports, or agent environments.

## Evaluation gates

- [ ] No-memory, explicit Turso lexical degradation, and Memvid use identical tasks, models, complete prompt budgets, chronological corpora, and fresh native sessions.
- [ ] Results report reviewer acceptance, repeated errors, rework, required verification, precision/recall, selected-memory usefulness, tokens, latency, CPU/RAM, disk, setup, and restart behavior.
- [ ] Hard gates are zero cross-project leaks, zero ineligible/tombstoned pack items, zero successful budget overflow, zero secret/transcript leakage, deterministic degraded output, and recovery after restart.
- [ ] Provider benefit claims are based on repeated measured results rather than provider marketing or incomparable benchmarks.
