# Memory retrieval provider contract V1

## Status

Design contract. No runtime types or adapters are implemented by this documentation task.

This is a provider-facing domain contract, not an agent wire format. It does not amend `AgentAssignmentV1`, `AgentResponseV1`, or `ConduitResultRecordV1`.

## Port ownership

`MemoryRetrievalProvider` belongs to the memory domain's interfaces. DTOs belong to memory-domain types. Memvid and Hindsight adapters live behind the memory-domain factory/registry. Communication providers, run handlers, TUI code, and agent prompts do not import these provider-facing contracts.

The TypeScript names below are normative concepts. Implementation must place shared types/interfaces/enums in the repository-prescribed domain directories rather than copying declarations into services or repositories.

## Descriptor and capabilities

```ts
type CapabilitySupport = "supported" | "unsupported" | "unknown";
type ProviderLifecycleMode = "embedded" | "managed-daemon" | "external-http";

interface MemoryProviderDescriptorV1 {
  contractVersion: "1.0";
  providerId: string;
  adapterVersion: string;
  providerRuntimeVersion: string;
  providerDataFormatVersion: string;
  lifecycleMode: ProviderLifecycleMode;
  configurationGeneration: string;
  namespaceIsolation: CapabilitySupport;
  lexicalSearch: CapabilitySupport;
  semanticSearch: CapabilitySupport;
  metadataFilters: CapabilitySupport;
  batchUpsert: CapabilitySupport;
  idempotentUpsert: CapabilitySupport;
  exactDelete: CapabilitySupport;
  fullRebuild: CapabilitySupport;
  checkpoint: CapabilitySupport;
  maxDocumentBytes: number;
  maxQueryBytes: number;
  maxResults: number;
  maxRebuildBatchDocuments: number;
  maxRebuildBatchBytes: number;
}

type DecimalSequenceV1 = string;

interface ProjectionWatermarkV1 {
  namespace: string;
  configurationGeneration: string;
  projectionGeneration: string;
  truthSequence: DecimalSequenceV1;
}

interface MemoryProviderHealthV1 {
  status: "ready" | "read-only" | "degraded" | "unavailable";
  appliedWatermark: ProjectionWatermarkV1 | null;
  activeProjectionGeneration: string | null;
  checkedAt: string;
  sanitizedReason: string | null;
}
```

Provider/version support comes from a checked-in frozen compatibility matrix. Adapters must not translate an unknown runtime/data-format version into `supported`. Unknown or incompatible versions are terminal and cannot request Turso lexical degraded mode.

## Projection DTO

```ts
interface MemoryProjectionDocumentV1 {
  contractVersion: "1.0";
  namespace: string;
  memoryId: string;
  memoryVersion: number;
  kind: string;
  logicalPath: string;
  summary: string;
  keywords: readonly string[];
  securityClass: string;
  canonicalDocumentHash: string;
}
```

The DTO contains compact, redacted, already active canonical data only. Evidence bodies, transcripts, prompts, complete files, logs, secrets, private reasoning, approval rationale, and provider credentials are forbidden.

## Operations

```ts
interface MemoryProviderCallContextV1 {
  contractVersion: "1.0";
  configurationGeneration: string;
  deadlineAt: string;
  signal: AbortSignal;
}

interface MemoryProjectionOperationV1 {
  operationId: string;
  namespace: string;
  operation: "upsert" | "delete";
  memoryId: string;
  memoryVersion: number;
  truthSequence: DecimalSequenceV1;
  targetProjectionGeneration: string;
  canonicalDocumentHash: string;
  document: MemoryProjectionDocumentV1 | null;
}

interface MemoryProjectionReceiptV1 {
  operationId: string;
  outcome: "applied" | "already-applied" | "rejected" | "retryable-failure";
  appliedWatermark: ProjectionWatermarkV1 | null;
  providerRevision: string | null;
  sanitizedReason: string | null;
}

interface MemorySearchRequestV1 {
  requestId: string;
  namespace: string;
  query: string;
  allowedKinds: readonly string[];
  limit: number;
  maxResponseBytes: number;
  requiredWatermark: ProjectionWatermarkV1;
  projectionGeneration: string;
}

interface MemorySearchCandidateV1 {
  memoryId: string;
  memoryVersion: number;
  channel: "lexical" | "semantic" | "hybrid" | "provider";
  score: number;
  providerRank: number;
}

interface MemorySearchResponseV1 {
  requestId: string;
  providerRevision: string;
  appliedWatermark: ProjectionWatermarkV1;
  candidates: readonly MemorySearchCandidateV1[];
  truncated: boolean;
}

interface MemoryRebuildRequestV1 {
  rebuildId: string;
  namespace: string;
  sourceSnapshotId: string;
  sourceSnapshotWatermark: ProjectionWatermarkV1;
  targetProjectionGeneration: string;
  documents: AsyncIterable<MemoryProjectionDocumentV1>;
}

interface MemoryRebuildReceiptV1 {
  rebuildId: string;
  outcome: "verified" | "rejected" | "retryable-failure";
  projectionGeneration: string | null;
  appliedWatermark: ProjectionWatermarkV1 | null;
  indexedDocuments: number;
  sanitizedReason: string | null;
}

interface MemoryRetrievalProvider {
  describe(
    context: MemoryProviderCallContextV1,
  ): Promise<MemoryProviderDescriptorV1>;
  health(context: MemoryProviderCallContextV1): Promise<MemoryProviderHealthV1>;
  apply(
    context: MemoryProviderCallContextV1,
    operation: MemoryProjectionOperationV1,
  ): Promise<MemoryProjectionReceiptV1>;
  search(
    context: MemoryProviderCallContextV1,
    request: MemorySearchRequestV1,
  ): Promise<MemorySearchResponseV1>;
  rebuild(
    context: MemoryProviderCallContextV1,
    request: MemoryRebuildRequestV1,
  ): Promise<MemoryRebuildReceiptV1>;
  checkpoint(context: MemoryProviderCallContextV1): Promise<void>;
  close(context: MemoryProviderCallContextV1): Promise<void>;
}
```

The concrete implementation may use bounded batches while preserving operation identities and receipts. `rebuild` may return rejected when the descriptor declares it unsupported, but every approved adapter must support exact deletion or a verified rebuild strategy.

## Normative Conduit hard bounds

Effective limits are the minimum of configured limit, Conduit's hard cap, and the selected provider's declared cap. Configuration cannot raise a Conduit/provider cap. Length uses UTF-8 bytes after normalization and redaction.

- identifier (request, operation, memory, generation, revision): 128 bytes;
- namespace: 192 bytes;
- logical path: 512 bytes;
- kind/security class: 64 bytes each;
- summary: 2,000 characters and 8,192 bytes;
- keywords: 32 items, 64 bytes each, 1,024 bytes total;
- canonical projection document: 16,384 bytes;
- allowed kinds per query: 16;
- queries per context pack: 8;
- one query: 2,048 bytes; all queries in one pack: 8,192 bytes;
- results per query: 50; unique candidates across a pack: 200;
- one search response: 65,536 bytes;
- rebuild batch: 256 documents and 2 MiB;
- one provider call: 5 seconds, except a configured rebuild call capped at 30 seconds per batch;
- sanitized diagnostic/reason: 1,000 characters and 4,096 bytes.

Provider responses that exceed a hard bound are invalid. Truncation is allowed only for a diagnostic string at a valid UTF-8 boundary with an explicit truncated marker; identifiers, documents, candidates, and watermarks are never repaired by truncation.

## Behavioral requirements

- `describe` and `health` are called before the first memory-enabled launch and after a relevant provider/configuration failure.
- Every call validates the common contract version, configuration generation, deadline, cancellation signal, and exact frozen runtime version. Data operations additionally validate namespace, byte/item bounds, target projection generation, and watermark/sequence.
- Deadline expiry or cancellation aborts provider work promptly and returns no partial success receipt. A managed daemon receives bounded protocol cancellation when supported, followed by bounded process termination.
- Applying the same operation ID repeatedly has the same logical result and does not increase relevance or duplicate a document.
- `truthSequence` is a canonical unsigned 64-bit integer encoded as a base-10 string with no sign or leading zero except `"0"`. Turso allocates it monotonically per namespace.
- A watermark is scoped by namespace, configuration generation, and projection generation. Two watermarks are comparable only when those three strings match; their sequence is compared as an integer, never lexicographically or as a JavaScript `number`.
- `appliedWatermark.truthSequence` is the greatest continuous canonical prefix for its scope: every canonical operation from the scope's first sequence through that value is either applied or recorded obsolete with proof of a newer canonical operation for the same memory identity. A missing, pending, retryable-failed, or terminal-failed operation blocks advancement past its sequence.
- Observing or applying sequence N+1 before N does not advance the watermark through N+1, even when the two operations affect different memory IDs. An individual operation receipt cannot advance the global watermark without the canonical outbox ledger proving the continuous prefix.
- For each scoped namespace and memory ID, an adapter must ignore/reject an operation older than the greatest applied truth sequence. An upsert at an older sequence can never overwrite a later delete/tombstone.
- Search waits for or proves `requiredWatermark`; a stale watermark is a provider failure, not a partial success.
- Search returns no prose answer. Any provider snippet is discarded before this DTO boundary.
- Candidate order is advisory. Conduit applies deterministic tie-breaking and canonical filtering.
- Unknown IDs, wrong versions, non-finite scores, duplicate candidates, excess results, cross-namespace results, or excess bytes reject or sanitize the response and emit a bounded conformance diagnostic.
- `close` is idempotent. Managed-daemon shutdown has a bounded grace period.
- Provider errors contain no document content, credentials, endpoint tokens, raw HTTP bodies, or transcripts.
- Transient availability, timeout, rate-limit, busy, query, and interrupted-I/O failures are retryable. Contract/schema/version/capability/namespace/authentication/secret-policy failures and unsupported operations are terminal.
- The Conduit outbox retries at most eight attempts and fifteen minutes elapsed with exponential backoff from 250 ms capped at 30 seconds and full jitter. Exhaustion writes terminal failed state; adapters do not retry internally beyond one bounded transport retry already counted by the outbox.

## Factory and configuration

The registry accepts the resolved provider ID and provider-specific validated options and returns exactly one adapter. It does not probe alternatives.

- `memvid` creates the embedded default projection using the configured project-local index path.
- `hindsight` creates the HTTP adapter using lifecycle, endpoint, bounded timeouts, and an opaque credential-profile/secret reference.

Secret values are resolved only inside adapter construction through the credential boundary. They are never fields of these DTOs.

## Conformance

Every adapter must pass the same contract suite for descriptor accuracy, unknown-version rejection, namespace isolation, bounds, exact ID/version round-trip, idempotent retries, watermark behavior, delete/rebuild, crash recovery, sanitized failures, and close. Memvid additionally passes native packaging and persistence on all four supported standalone targets. Hindsight additionally passes HTTP timeout/cancellation, daemon lifecycle when enabled, cross-bank rejection, and synthesized-answer exclusion.
