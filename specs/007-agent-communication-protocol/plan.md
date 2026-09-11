# Feature 007 plan

## Delivered protocol

The universal V1 assignment, response, result-record, runner parsing, semantic validation, rendered research, and completion semantics are implemented. Their schemas remain unchanged.

## Proposed long-term memory implementation sequence

### 007-M0: Contract, configuration, and compatibility gate

1. Add memory-domain enums, types, interfaces, errors, commands, queries, and read models matching `contracts/memory-provider-v1.md`.
2. Extend existing global settings, project `conduit.yml`, effective settings, validation, serialization, and field provenance for memory configuration. Use project over global settings over built-in defaults; add no environment or CLI override in this group. Keep shipped memory disabled throughout partial groups.
3. Add a factory/registry that selects exactly the configured provider.
4. Freeze an exact Memvid SDK/runtime/data-format version and run the compatibility spike for NodeNext, Bun, lifecycle, concurrency, crash behavior, ID/version round-trip, delete/rebuild, and all four standalone targets.
5. Freeze the optional Hindsight HTTP/API compatibility matrix and prove bounded candidate-only behavior.

This group is a release gate. If Memvid fails a required target or behavior, record a blocker; do not silently substitute Turso or Hindsight.

Checks:

- `pnpm --filter conduit-orchestrator test -- memory-contracts`
- `pnpm --filter conduit-orchestrator test -- memory-configuration`
- `pnpm --filter conduit-orchestrator test -- memory-provider-compatibility`
- `pnpm --filter conduit-orchestrator typecheck`
- `pnpm --filter conduit-orchestrator lint`

### 007-M1: Canonical lifecycle, immutable provenance, and evidence

1. Add project/global memory migrations through the existing ordered migration registry.
2. Persist immutable accepted-response snapshots and proposals before the latest result read-model upsert.
3. Implement idempotent proposal ingestion, validation, redaction, deterministic eligibility, duplicate/contradiction handling, and lifecycle decisions.
4. Implement typed evidence snapshots and commit/contract/verification/promotion validity.
5. Keep truth lifecycle separate from provider projection status.
6. Add explicit decision handling for ambiguous project proposals and explicit global-promotion approval.

Checks:

- `pnpm --filter conduit-orchestrator test -- memory-lifecycle`
- `pnpm --filter conduit-orchestrator test -- memory-provenance`
- `pnpm --filter conduit-orchestrator test -- memory-evidence`
- `pnpm --filter conduit-orchestrator test -- memory-security`
- `pnpm --filter conduit-orchestrator typecheck`

### 007-M2: Projection outbox and providers

1. Add transactional outbox, projection ledgers, stable operation IDs, retries, watermarking, tombstones, and rebuild generations.
2. Implement the Memvid adapter as the required default projection/search provider.
3. Implement Hindsight as the explicitly configured daemon/HTTP alternative.
4. Implement explicit Turso lexical degraded mode only for transient availability/query/I/O/watermark failures from a version-compatible provider.
5. Add bounded retry classification, attempt/elapsed caps, backoff/jitter, terminal failure ledger, and synchronization-deadline handling.
6. Add common adapter conformance, fault injection, crash recovery, deletion, and namespace-isolation coverage.

Checks:

- `pnpm --filter conduit-orchestrator test -- memory-outbox`
- `pnpm --filter conduit-orchestrator test -- memory-provider-conformance`
- `pnpm --filter conduit-orchestrator test -- memory-memvid`
- `pnpm --filter conduit-orchestrator test -- memory-hindsight`
- `pnpm --filter conduit-orchestrator test -- memory-degraded-mode`
- `pnpm --filter conduit-orchestrator typecheck`
- `pnpm --filter conduit-orchestrator lint`

### 007-M3: Retrieval, snapshots, context compiler, and delivery

1. Implement launch-time canonical snapshots, provider-neutral query construction, exact/deterministic eligibility, canonical hydration, ranking, contradiction handling, attribution, and omission records.
2. Enforce the complete Conduit-authored prompt budget and reserved output budget; block mandatory overflow.
3. Materialize one immutable transient context-pack file per assignment attempt, append its exact path to unchanged `AgentAssignmentV1.contextReferences`, and add write protection through `forbiddenPaths` and execution policy.
4. Integrate compilation after dependencies complete and before each initial, retry, correction, re-review, or resumed launch.
5. Exclude registered transient files from Git integration and remove them after the turn/crash-retention window.

Checks:

- `pnpm --filter conduit-orchestrator test -- memory-retrieval`
- `pnpm --filter conduit-orchestrator test -- memory-context-budget`
- `pnpm --filter conduit-orchestrator test -- memory-context-snapshots`
- `pnpm --filter conduit-orchestrator test -- memory-context-delivery`
- `pnpm --filter conduit-orchestrator test -- no-agent-memory-access`
- `pnpm --filter conduit-orchestrator typecheck`

### 007-M4: Cache, invalidation, deletion, and global promotion

1. Add complete pack-cache keys and generation-based invalidation.
2. Propagate evidence/contract/configuration/dependency/provider changes without reusing stale packs.
3. Implement inspect, explain, invalidate, supersede, expire, tombstone, provider deletion acknowledgement, and verified rebuild.
4. Implement generalized global promotion with explicit approval, separate provenance/projection, revocation, and deletion.

Checks:

- `pnpm --filter conduit-orchestrator test -- memory-cache`
- `pnpm --filter conduit-orchestrator test -- memory-invalidation`
- `pnpm --filter conduit-orchestrator test -- memory-deletion`
- `pnpm --filter conduit-orchestrator test -- memory-global-promotion`
- `pnpm --filter conduit-orchestrator typecheck`

### 007-M5: Evaluation and release qualification

1. Compare no-memory, explicit Turso lexical degradation, and Memvid using identical tasks, models, total prompt budgets, fresh native sessions, and chronological corpus splits.
2. Measure reviewer acceptance, repeated errors, rework, verification, retrieval precision/recall, tokens, latency, CPU/RAM, disk growth, setup, and restart behavior.
3. Verify zero cross-project leaks, invalid/tombstoned inclusions, budget overflow, transcript/secret leakage, and nondeterministic degraded output.
4. Run package and standalone qualification on every supported target.
5. Only after every M0-M5 gate passes, switch the shipped target default to memory enabled with Memvid and degraded mode disabled.

Checks:

- `pnpm --filter conduit-orchestrator test`
- `pnpm --filter conduit-orchestrator typecheck`
- `pnpm --filter conduit-orchestrator lint`
- `pnpm --filter conduit-orchestrator build`
- `pnpm start --help`
- `pnpm build:standalone -- linux-x64`
- `pnpm build:standalone -- linux-arm64`
- `pnpm build:standalone -- darwin-arm64`
- `pnpm build:standalone -- windows-x64`
- `pnpm --filter conduit-orchestrator test -- standalone-persistence` on the corresponding native CI runner for each target
- memory-provider standalone conformance on the corresponding native CI runner for each target

## Migration order

Exact migration IDs are assigned at implementation time after the current registry. The order is:

1. immutable accepted responses, proposals, and proposal decisions;
2. memory identities, versions, relations, and evidence;
3. provider projection state and outbox;
4. retrieval attribution and immutable context packs/items;
5. cache/invalidation generations and deletion acknowledgements;
6. global promotion provenance and global memory lifecycle in the global database.

Existing `result_records`, `source_versions`, run history, runtime events, and workspace lineage remain readable. A migration must not reinterpret them as active memory.

## Failure recovery

- Canonical transaction failure creates no partial activation or outbox event.
- Duplicate response delivery and outbox retry are idempotent.
- Provider failure leaves Turso truth intact.
- Interrupted rebuild generations remain inactive.
- Projection deletion lag cannot make truth eligible.
- Mandatory budget overflow, invalid evidence, incompatible provider version, namespace violation, and canonical database failure block launch.
- Explicit Turso lexical degraded mode handles only transient availability/query/I/O/watermark failure from an already version-compatible provider and records the degradation.
- Unknown/incompatible versions and contract/capability/configuration/authentication failures are terminal and never degrade.
