# Feature 007: Agent communication protocol and long-term memory

## Status

The universal V1 communication protocol is implemented. The long-term memory extension in this packet is approved design scope and has not been implemented. Its task groups remain unchecked in `tasks.md`.

## Outcome

Conduit gives each independently launched role a bounded snapshot of verified prior knowledge and accepts structured proposals for later launches. Turso remains the canonical source of truth. Memvid is the required default retrieval projection. Hindsight is an explicitly configured alternative. Agents and the communication layer never know which retrieval backend is selected, never query a database or memory service, and never activate memory.

The product contract is:

> A later independent role can use a verified lesson from an earlier result without receiving that result's transcript, violating the current feature contract, or exceeding the complete Conduit-authored context budget.

## Implemented V1 protocol

Conduit uses `AgentAssignmentV1` for every role input and one universal `AgentResponseV1` for every role output. `ConduitResultRecordV1` wraps the response with Conduit-owned process, run, role, runner, file-observation, event, and validation metadata.

The schemas under `contracts/` are frozen for this feature:

- `agent-assignment-v1.schema.json`;
- `agent-response-v1.schema.json`;
- `conduit-result-record-v1.schema.json`.

The memory extension MUST preserve those files byte-for-byte. It MUST NOT add another handoff envelope. Existing `AgentResponseV1.memoryProposals` and `globalPromotionProposals` remain the only agent-authored memory inputs. Their intentionally small shapes are compatible ingestion inputs, not active-memory records.

The required separation remains:

1. runner-native streaming transport in system communication adapters;
2. normalized Conduit real-time events with explicit provenance;
3. one strict universal final response;
4. assignment-specific semantic validation;
5. authoritative Conduit result metadata;
6. human-readable reports rendered from validated data;
7. a memory domain that consumes accepted results and produces bounded context references without exposing its storage or provider.

### Completion and trust

A role completes successfully only when the process outcome is acceptable, a final response is captured, structural/security/path/size validation succeeds, assignment semantic validation succeeds, and response status is `completed`. Zero exit with missing, malformed, partial, blocked, `needs_input`, or failed protocol output is not successful completion. Useful validated failure results may be retained, but do not unlock dependents.

Agent artifacts, verification, evidence, memory proposals, and global-promotion proposals are claims. Conduit-observed process, Git, package, contract, and approval records decide whether a claim is eligible for memory. Valid JSON alone never establishes truth.

## Memory boundaries

### Canonical truth

Project Turso `state.db` is canonical for project proposals, evidence, memory versions, approvals/decisions, projection outbox state, retrieval attribution, and context-pack snapshots. The global Turso database is canonical only for explicitly approved generalized global memories and their provenance.

A retrieval provider is a rebuildable projection and candidate generator. It cannot activate, amend, supersede, expire, tombstone, promote, or authorize a memory. Provider output is never inserted directly into an agent prompt. Conduit resolves every returned memory ID and version against Turso and rechecks lifecycle, project identity, evidence validity, security, and snapshot eligibility before selection.

Raw transcripts, raw prompts, private reasoning, complete source files, unredacted logs, credentials, secret values, environment dumps, provider-generated reflections, and unvalidated response content MUST NOT enter canonical memory, provider projections, caches, or context packs.

### Scope and kinds

The canonical domain supports:

- `run`: bounded observations useful only to the current run;
- `feature`: knowledge bound to a feature packet/package hash;
- `project`: durable knowledge bound to a repository identity;
- `global`: explicitly approved, generalized knowledge in the global database.

Memory kinds are `fact`, `decision`, `contract`, `convention`, `command`, `pitfall`, `correction`, `observation`, and `promoted-lesson`.

Stable logical addresses use Conduit-owned identities, for example:

```text
project://<repository-id>/contracts/api/orders
feature://007/<package-hash>/decisions/memory-provider
run://<run-id>/roles/qa/failure-lessons
global://typescript/nodenext/import-specifiers
```

Role IDs, native session IDs, worktree paths, and repository basenames are not project identity.

## Canonical lifecycle and persistence

### Immutable proposal ingestion

The current `result_records` table is a read model keyed by `(run_id, role_id)`; a later retry or correction can replace the stored JSON for that pair. Memory provenance MUST NOT point only to that mutable row.

After a response passes structural, security, path, size, and assignment-semantic validation, Conduit MUST, before updating the latest-result read model:

1. canonicalize the accepted response and compute `responseHash`;
2. append an immutable accepted-response snapshot identified by assignment ID and response hash;
3. append each proposal with its zero-based ordinal;
4. copy Conduit-owned run, role, role kind, feature, task, package hash, repository identity, starting HEAD, workspace generation, observed commit/promotion state, process outcome, and validation metadata;
5. copy only bounded redacted proposal content and evidence references;
6. hash a length-delimited tuple of repository ID, run ID, assignment ID, response hash, and proposal ordinal as the idempotency identity.

The tuple hash is SHA-256 over each UTF-8 field encoded as its decimal byte length, one colon, then the exact bytes, in the stated order; the ordinal uses its canonical unsigned decimal string. This prevents delimiter ambiguity. Repeated delivery of the same accepted response is a no-op. A retry or correction has a new assignment ID and therefore distinct immutable provenance. Deduplication may link equivalent proposals, but MUST NOT erase their separate origins.

A response that fails structural, security, path, size, or assignment-semantic validation creates no proposal, including a response that falsely claims completion. A semantically accepted `failed`, `blocked`, or `partial` result may contribute a narrowly scoped failure lesson only when its status fields are internally valid and Conduit independently observed the failure evidence. A non-completed result cannot assert that unfinished implementation is current project truth or unlock dependents.

### Proposal decisions

Project proposals begin as `pending`. Conduit validates redaction, bounds, scope, duplicates, contradictions, evidence, current contract applicability, and result acceptance before it may activate a project memory.

An evidence reference is optional in the frozen V1 response contract. A proposal with no independently resolvable evidence remains pending and MUST NOT autoactivate. Agent-authored evidence text by itself is not independent evidence.

An eligible project proposal may activate through a deterministic Conduit policy when every claim is supported by current independently verified evidence and the result state permits that claim. Ambiguous, contradictory, sensitive, externally sourced, or policy-unsupported proposals remain pending for an explicit human/local operator decision or a separately enabled Conduit approval policy that the operator authorized in configuration. An architect-role `AgentResponseV1` is still an agent proposal and cannot approve itself. Agents cannot make lifecycle decisions.

Global promotion is always separate and requires an explicit audited approval from a local or authenticated human command/UI action, or from a separately configured approval policy that a human previously authorized for that scope. Agent role, verdict, or response content is never approval. A project activation decision does not authorize global promotion.

### Truth lifecycle

Canonical memory truth states are:

```text
pending proposal -> active version
pending proposal -> rejected
active version -> stale | superseded | expired | tombstoned
```

Corrections append a new version and link the prior version as superseded. Historical versions and proposal decisions remain auditable. Only active, unexpired, evidence-valid versions are eligible for context.

Truth status is independent from retrieval projection and embedding status. Each provider projection record has a separate status such as `pending`, `indexed`, `failed`, `delete-pending`, or `deleted`, plus provider ID, adapter contract version, provider SDK/runtime version, projection document hash, provider index revision, attempts, and sanitized last error. When a provider uses embeddings, its representation also records `not-required`, `pending`, `ready`, `failed`, `incompatible`, `delete-pending`, or `deleted` plus model/runtime identity and canonical document hash. An indexed record can be ineligible truth; an active truth can temporarily lack a projection or embedding. Embedding failure never changes canonical truth, and an incompatible representation is excluded rather than interpreted with a new model.

### Canonical tables

Migrations may normalize names, but the model MUST preserve equivalent records for:

- immutable accepted response snapshots;
- memory proposals and proposal decisions;
- memory identities and append-only versions;
- evidence snapshots and version-evidence links;
- relations and supersession;
- global promotion requests and decisions;
- provider projection state;
- transactional projection outbox entries;
- retrieval requests, candidates, attribution, and diagnostics;
- immutable context packs and context-pack items;
- cache keys and invalidation generations.

No canonical lifecycle field is stored only in a Memvid file or Hindsight bank.

## Evidence and commit validity

Evidence snapshots use typed Conduit-owned references. Supported evidence classes include:

- approved feature contract: repository identity, relative path, feature package hash, Git blob/content hash, and approval state;
- source artifact: repository identity, commit OID, relative path, blob OID/content hash, and observed ownership;
- verification observation: persisted Conduit event/result identity, operation, process exit, bounded sanitized evidence, and associated commit/workspace identity;
- promotion observation: approved reviewer result plus promoted project commit OID;
- explicit decision: Conduit decision ID, actor class, scope, timestamp, and bounded rationale;
- prior memory: exact memory ID/version and its still-valid evidence.

A path, role name, mutable branch name, native session ID, agent claim, or latest `result_records` row alone is not durable evidence.

Commit-backed evidence is valid only when the repository identity matches, the commit resolves as a commit object, the referenced blob/path resolves at that commit, recorded hashes match, and its acceptance class still supports the claim:

- a claim about current project behavior requires the evidence commit to be the approved promoted commit or an ancestor whose relevant content still matches the current approved HEAD;
- a feature-contract claim also requires the current approved package hash or an explicit compatibility relation;
- a failure/correction lesson may reference a retained unpromoted commit or observed failed command, but its text and applicability MUST say that it is a failure lesson and MUST NOT present the branch as current project state.

Worktree cleanup does not invalidate evidence when the exact commit, blob, hashes, promotion record, and repository identity remain verifiable. Missing Git objects, changed required content, a superseding contract, revoked approval, expiry, project-identity mismatch, or unverifiable observation makes the dependent memory stale before retrieval.

Evidence validity and projection freshness are evaluated separately. Re-embedding or reindexing cannot make stale evidence true.

## Provider-neutral retrieval contract

The memory domain owns a `MemoryRetrievalProvider` port and DTOs described in `contracts/memory-provider-v1.md`. Communication providers, runners, run handlers, TUI components, and agents MUST depend only on memory queries/context-pack read models. They MUST NOT import Memvid or Hindsight adapters or branch on provider IDs.

Only a memory-domain factory/registry may map the resolved provider ID to an adapter. Selection is exact. If `memvid` is selected, Conduit never tries Hindsight. If `hindsight` is selected, Conduit never opens Memvid. Unknown or disabled IDs fail configuration validation.

Providers receive only canonical projection documents containing opaque project namespace, memory ID, version, kind, logical path, compact verified summary, normalized keywords, security class, and canonical document hash. They do not receive raw evidence bodies or transcripts.

Search responses contain bounded candidate keys, provider channel, score, and provider revision. Provider snippets or generated answers are diagnostic only and are never trusted as memory content. Conduit reloads canonical content from Turso.

All provider/configuration bounds use the minimum of the configured value, the normative Conduit hard cap, and the provider descriptor cap. `contracts/memory-provider-v1.md` fixes identifier/document/query/result/rebuild/diagnostic sizes and permits at most eight queries per pack. Configuration cannot raise either hard boundary.

### Capability negotiation

Before the first memory-enabled role launch, the selected adapter MUST report:

- adapter contract version;
- exact provider SDK/runtime version and data-format/API version;
- lifecycle mode: embedded, managed daemon, or external HTTP;
- health and writable/read-only state;
- maximum query/result/document bounds;
- support status for lexical search, semantic search, metadata filters, batch upsert, idempotent upsert, exact delete, full rebuild, checkpoint, and namespace isolation.

Capability values are `supported`, `unsupported`, or `unknown`. Required capabilities cannot be inferred from method presence. The selected adapter's frozen compatibility matrix decides which exact versions are allowed. Unknown versions fail closed.

Baseline requirements are namespace isolation, bounded search, exact memory ID/version round-trip, deterministic tie handling at the Conduit layer, idempotent projection, deletion or verified full rebuild, health reporting, and a projection revision/watermark. Semantic search is optional; provider relevance never bypasses canonical eligibility.

### Required default: Memvid

The post-qualification target default is:

```yaml
memory:
  enabled: true
  retrievalProvider: memvid
  degradedMode: disabled
  contextBudgetTokens: 24000
  reservedOutputTokens: 4000
  maxQueriesPerPack: 8
  maxResults: 20
  providers:
    memvid:
      indexPath: .conduit/cache/memory/project.mv2
```

Until every 007-M0 through 007-M5 release gate passes, shipped memory remains disabled and partial implementations MUST NOT change launch behavior. Enabling the completed feature switches to the target default above; it does not select a partial provider or implicit Turso retrieval.

Memvid is a rebuildable project-local projection. Turso remains canonical. The adapter owns creation, locking, checkpointing, crash recovery, rebuild, and deletion verification for the `.mv2` file. It stores only the projection DTO. Provider-native generated or synthesized text is never activated.

Memvid MUST NOT become the default in a release until a compatibility spike freezes an exact SDK version/commit and data format and proves NodeNext/Bun behavior, read-after-write, interrupted writes, locking, exact ID/version round-trip, bounded search, deletion or rebuild, and standalone packaging on all four supported release targets:

- `linux-x64`;
- `linux-arm64`;
- `darwin-arm64`;
- `windows-x64`.

The same exact dependency set must pass cold-start and persistence tests without `node_modules`. Vector/ONNX features are disabled unless separately version-frozen and proven on all four targets. Failure on any target blocks shipping this feature with memory enabled by default; it does not authorize changing the default provider.

### Configurable alternative: Hindsight

Hindsight is selected only through effective Conduit configuration:

```yaml
memory:
  retrievalProvider: hindsight
  degradedMode: disabled
  providers:
    hindsight:
      lifecycle: external
      endpoint: http://127.0.0.1:8888
      credentialProfile: hindsight-local
      requestTimeoutMs: 2000
```

The adapter communicates through the frozen, version-checked HTTP contract whether the daemon is operator-managed or Conduit-managed. Managed lifecycle, if approved later, uses a process-scoped child with bounded startup, health, cancellation, and shutdown; it MUST NOT edit global/project Hindsight configuration or expose credentials in arguments/logs.

Only credential-profile or secret-reference names may appear in configuration. Secret values remain in the credential boundary and never enter `conduit.yml`, global settings, Turso memory rows, provider documents, diagnostics, prompts, or agent environments.

Hindsight retain/recall capabilities may implement projection and candidate search. Reflect or other synthesized answers are not memory truth and MUST NOT enter packs. Results are capped by Conduit's `maxResults`, response bytes, timeout, namespace, and allowed memory ID/version set. Returned unknown IDs, duplicate IDs, stale versions, and cross-project results are discarded and recorded as sanitized provider violations.

### Explicit Turso lexical degraded mode

Turso lexical retrieval is a safety degradation path, not the default projection and not a second automatically selected provider. Built-in `degradedMode` is `disabled`.

An operator may explicitly set:

```yaml
memory:
  retrievalProvider: memvid
  degradedMode: turso-lexical
```

When a version-compatible selected provider has a transient availability, timeout, query, I/O, or synchronization/watermark failure, Conduit may then generate bounded lexical candidates from active canonical Turso summaries. Every affected pack and run diagnostic records `turso-lexical`, the selected provider, and sanitized reason in internal audit metadata. Conduit does not silently try Hindsight or claim semantic-provider capability.

With degraded mode disabled, selected-provider failure blocks the memory-enabled role launch before the agent receives context. Unknown/incompatible provider versions, capability/configuration/authentication failures, security, namespace, evidence, budget, or canonical database failures always fail closed and cannot use degraded mode.

## Configuration and provider lifecycle

Memory configuration extends the existing configuration domain. Precedence for memory fields is:

1. project `conduit.yml` memory fields;
2. user-global `settings.yml` memory fields;
3. built-in defaults.

This feature adds no environment-variable override and requires no new CLI override. Existing role global-profile resolution does not override project/global memory fields. Resolution is field-by-field, validates provider-specific options only for the selected provider, and records provenance for every effective memory setting. Changing provider, namespace, index format, canonical document format, or relevant ranking/budget settings increments a configuration generation and invalidates affected projections or packs.

Provider lifecycle is application-owned:

- initialize after effective configuration and canonical migrations;
- negotiate capabilities before memory-enabled launch;
- synchronize the outbox to the required snapshot watermark;
- search within explicit request bounds;
- checkpoint/flush before clean shutdown when supported;
- release locks or terminate a managed daemon within a bounded grace period;
- retain sanitized health and retry diagnostics.

Agents cannot start, stop, configure, or call a provider.

## Projection, outbox, deletion, and rebuild

Every canonical activation, supersession, stale transition, expiry, tombstone, or generalized global-memory change writes the truth change and a projection outbox entry in one Turso transaction.

Outbox entries have a stable operation ID, namespace, memory ID/version, operation (`upsert`, `delete`, or `rebuild`), canonical document hash, target provider/config generation, monotonic canonical truth sequence, attempt count, next attempt, and state. Workers reuse the same operation ID across retries. Provider adapters MUST treat repeated upserts/deletes idempotently or Conduit MUST prove equivalent behavior through a shadow ledger and rebuild.

Canonical Turso allocates an unsigned 64-bit sequence per namespace and stores it as a base-10 string so JavaScript number precision is irrelevant. A projection watermark is the structured tuple `namespace + configurationGeneration + projectionGeneration + truthSequence`. Watermarks are comparable only when the first three fields are equal; sequence comparison parses the decimal string as an integer.

The applied watermark is the greatest continuous canonical prefix in that scope. Every operation through the watermark must be applied or recorded obsolete with proof of a newer canonical operation for the same memory identity. Missing, pending, retryable-failed, or terminal-failed operations block advancement. Applying N+1 for one memory before N for another does not cover a snapshot at N+1 until N is accounted for. Per-memory ordering remains an additional guard: an older retried upsert can never undo a newer stale/tombstone delete.

Only transient availability, timeout, rate-limit, busy, query, and interrupted-I/O classes are retryable. Schema/contract/version/capability/namespace/authentication/secret-policy violations and unsupported operations are terminal. A projection operation receives at most eight attempts and at most fifteen minutes elapsed, whichever occurs first, using exponential backoff from 250 ms capped at 30 seconds with full jitter. Exhaustion writes a terminal failed ledger record with sanitized reason and requires explicit retry/rebuild/operator action.

Before launch, the outbox worker receives a bounded synchronization deadline and must make the selected provider's applied watermark cover the context snapshot. A lagging projection is never queried. A transient inability to reach the watermark may use explicitly configured Turso lexical degraded mode; otherwise launch blocks with the terminal/pending ledger state. Unknown/incompatible versions and non-transient failures never degrade.

Deleting or invalidating canonical truth immediately makes it ineligible even if provider deletion is pending. Cache lookup and search candidate hydration always recheck truth. Tombstones remain until every configured projection and cache generation acknowledges deletion or a verified rebuild excludes the record. A provider returning a deleted/stale version is a conformance violation and cannot resurrect it.

A full rebuild reads only eligible canonical projection documents at one Turso snapshot and records its source watermark, writes a new provider generation, then applies or proves every later outbox operation through the activation watermark. Conduit verifies counts, hashes, deletion coverage, and sample round-trips before atomically making that generation queryable. A rebuild based before a newer tombstone cannot activate without applying that tombstone. Interrupted or causally stale generations never replace the last verified projection.

## Retrieval and context compilation

### Snapshot and candidate pipeline

Context is compiled immediately before each role launch, after its dependencies and any eligible proposal activation for those dependencies have completed. The compiler opens a consistent Turso snapshot containing:

- repository identity and approved project HEAD;
- feature package hash and applicable contract hashes;
- assignment ID, role kind, owned/forbidden paths, dependencies, and verification requirements;
- active memory IDs/versions and evidence-validation generation;
- resolved memory configuration and provider capability generation;
- provider projection watermark;
- global-memory approval generation.

The immutable canonical context-pack audit record stores those values. A separate delivered payload contains only assignment-authorized mandatory context, canonical verified memory text/attribution, and provider-neutral notices that optional memory retrieval was degraded or omitted. Concrete provider IDs, revisions, scores, capability details, and failure reasons remain internal audit metadata and never appear in the delivered Markdown. A running role never receives newly activated memory mid-turn. A retry, correction, re-review, resume, or later dependency launch has a new assignment ID and a newly compiled immutable pack. Completed-role results and prior pack records remain unchanged.

Retrieval proceeds in this order:

1. reserve assignment, response contract, security, role guidance, acceptance criteria, applicable contracts, ownership, dependency handoff, and required verification as mandatory context;
2. fail before provider search if mandatory material cannot fit;
3. build bounded provider-neutral queries and eligibility filters;
4. request at most the configured provider candidate count;
5. hydrate candidate IDs/versions from the same canonical snapshot;
6. reject inactive, stale, expired, unauthorized, wrong-project, wrong-version, invalid-evidence, or contradictory candidates;
7. combine exact contract/path/dependency matches, provider scores, evidence strength, role/path relevance, and modest recency using a documented deterministic ranking;
8. deduplicate and preserve useful kind diversity;
9. add complete optional claims until the remaining budget is exhausted;
10. record selected and omitted items, reasons, channels, scores, evidence, token cost, and provider/degraded status.

Exact applicable contracts and explicit current decisions outrank similarity. A contradiction is a blocker or omission diagnostic; ranking never chooses a convenient truth.

### Hard complete-context budget

`contextBudgetTokens` is a hard budget for all Conduit-authored input supplied for one turn, including:

- serialized `AgentAssignmentV1`;
- Conduit system/role contract and project role guidance;
- response-schema/tool instructions;
- approved packet and contract text;
- dependency/reviewer/retry instructions;
- the memory pack and attribution labels;
- transport boilerplate and separators.

`reservedOutputTokens` is reserved separately from model capacity. Token counts use the selected model's verified tokenizer when available; otherwise Conduit uses a documented conservative bound and records the method. Estimated and actual serialized counts are both stored.

Mandatory content is compacted only through approved deterministic representations. It is never cut mid-claim. If mandatory content plus output reserve exceeds the configured/model limit, launch fails with a bounded overflow report identifying the contributors. Optional memory is omitted as whole items. Raising the budget requires configuration; the compiler cannot silently exceed it.

### Delivery through the existing assignment

No V1 schema change is required. The compiler materializes the immutable, redacted context snapshot as a read-only transient file inside the role workspace at a Conduit-reserved path equivalent to:

```text
.conduit-context/<assignment-hash>/context-pack-v1.md
```

The exact relative file path is appended to the existing `AgentAssignmentV1.contextReferences` array and its write-protected path is also appended to `forbiddenPaths`; the latter forbids agent mutation, not assignment-authorized reading. Broad contract references may remain where needed, but the assignment prompt explicitly identifies the generated pack as the authoritative bounded snapshot. The execution sandbox/permission policy makes the file read-only to the role. It is registered as Conduit-owned transient state, excluded from Git integration and changed-file ownership, inaccessible to sibling worktrees, and removed after the turn or by bounded crash cleanup. Its content hash must match the immutable Turso pack record before provider launch.

Harness adapters deliver the unchanged assignment through Feature 008. They do not query memory and do not receive provider credentials. Agents may read only the transient pack and ordinary assignment-authorized repository paths; they never receive a database path, provider endpoint/token, outbox, or raw retrieval response.

## Cache and invalidation

Pack cache keys include assignment identity, repository identity, approved HEAD, feature package/contract hashes, role/ownership/security inputs, dependency result IDs, active memory versions, evidence generation, global approval generation, effective configuration hash, provider capability/config generation, projection watermark, budget/tokenizer version, and compiler version.

Cache content is immutable and bounded. A hit still verifies project identity, truth eligibility, evidence generation, provider/degraded status, and content hash. Relevant contract/source changes, activation/supersession/staleness/tombstone, global revocation, configuration/provider changes, dependency result changes, tokenizer/compiler changes, or deletion increments a generation that prevents stale reuse.

Caches never hold raw transcripts, secret values, or provider-generated facts. Cache deletion follows canonical tombstones and cannot restore a removed version.

## Global promotion

An agent's `globalPromotionProposals` entry is only a proposal. Explicit approval creates a new generalized global memory; it is not a row copy.

Promotion MUST:

- start from an active evidence-valid project memory;
- record the approval decision and immutable project memory ID/version provenance;
- remove repository paths, customer/company identifiers, feature-specific details, raw evidence, and secret-like values;
- rerun validation and redaction;
- write a separate global logical identity/version and projection operation;
- support revocation, staleness, supersession, tombstone, and provider deletion;
- never make project deletion expose copied raw evidence.

Project and global namespaces are queried separately. Global candidates remain subject to role, security, evidence, and budget filters and normally rank below an exact project contract.

## Security and failure behavior

- Agents receive no Turso/libSQL variables, database paths/tokens, provider endpoints/tokens, credential values, or provider tools.
- Views dispatch commands/queries and render read models; they do not read databases/files or call providers.
- State changes use memory commands/handlers; reads use memory queries/handlers.
- Redaction happens before canonical persistence, projection, cache, diagnostics, or transient pack materialization.
- Provider documents and request/response diagnostics are bounded.
- Cross-project/namespace output, unknown IDs, incompatible provider versions, budget overflow, invalid evidence, and canonical storage failure fail closed.
- Provider relevance cannot grant instruction authority. Memory text is quoted as attributed context beneath the current assignment and contracts.
- No provider failure may cause an automatic Memvid-to-Hindsight or Hindsight-to-Memvid switch.

## Compatibility

- Existing V1 JSON schemas and stored V1 result records remain readable and unchanged.
- Existing results are not retroactively treated as active memory. Backfill, if later approved, creates immutable proposals with explicit provenance and applies the same evidence rules.
- Feature 008 harness selection, response capture, retry/correction, and transcript rules remain unchanged.
- Feature 009 workspace slots are execution continuity, not a memory corpus. Memory snapshots use their verified lineage only as provenance.
- The memory extension adds domain migrations and context references; it does not add a provider integration to the communication layer.

## Acceptance criteria

1. The three universal V1 schemas are byte-for-byte unchanged and no second handoff envelope exists.
2. Only structurally, securely, path/size, and assignment-semantically accepted responses create proposals; their length-delimited repository/run/assignment/response/ordinal identities and provenance remain immutable after `result_records` upserts.
3. Evidence-optional or agent-only claims never autoactivate; eligible project activation requires independently verified evidence and accepted/observed result semantics.
4. Failed-run lessons are explicitly scoped and never claim unpromoted work is current project truth.
5. Turso is canonical for lifecycle, evidence, decisions, outbox, retrieval attribution, internal pack audit, and delivered-payload snapshots.
6. Truth lifecycle, provider projection status, and embedding/representation status are separate and observable.
7. Shipped memory stays disabled through partial M0-M5 work; after every gate passes, Memvid is the enabled required target default with degraded mode disabled.
8. Hindsight is selected only by configuration and uses the same provider-neutral port with bounded ID/version results.
9. The communication layer and agents do not know the concrete provider and cannot access storage, providers, or credentials.
10. Capability/version/configuration/authentication mismatch fails closed without lexical degradation; provider selection never silently changes.
11. Turso lexical retrieval occurs only when `degradedMode: turso-lexical` is explicitly effective and a compatible provider has a transient availability/query/I/O/watermark failure; internal audit records the reason while delivered context gives only a provider-neutral notice.
12. Provider candidates are rehydrated and revalidated from the same canonical snapshot before prompt inclusion.
13. Projection writes/deletes are transactional-outbox driven, idempotent across bounded retries, ordered by scoped decimal-string truth sequence/watermark, rebuildable, and cannot resurrect tombstoned memory.
14. Commit, contract, verification, failure, and promotion evidence follow the specified validity rules through worktree cleanup and source change.
15. Context snapshots are immutable per assignment launch; later roles see only memory activated before their own snapshot.
16. The complete Conduit-authored context plus reserved output obeys a hard budget; mandatory overflow blocks launch and optional claims are omitted whole.
17. The pack reaches the agent through an exact transient path in unchanged `AgentAssignmentV1.contextReferences`, is write-protected through `forbiddenPaths` and execution policy, matches its persisted hash, and is excluded from integration.
18. Cache keys cover truth, evidence, contracts, dependencies, provider/configuration, projection watermark, budget/tokenizer, and compiler generations.
19. Global promotion requires an audited human action or separately human-authorized policy, generalizes content, preserves provenance, and supports revocation/deletion; an agent role cannot approve itself.
20. Provider calls enforce the minimum of configured, normative Conduit, and advertised provider bounds, including at most eight queries per pack.
21. Internal pack audit stores provider/retrieval diagnostics while delivered Markdown contains only provider-neutral degraded/omitted notices.
22. Retryable and terminal provider failures follow the bounded class, attempt, elapsed-time, backoff/jitter, ledger, and synchronization-deadline rules.
23. Seeded secrets and raw transcripts appear in none of Turso memory rows, provider documents, caches, packs, diagnostics, or agent environments.
