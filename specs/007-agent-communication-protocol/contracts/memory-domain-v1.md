# Memory domain contract V1

## Status and boundary

Design contract. These concepts are not executable TypeScript and do not amend the frozen agent V1 schemas.

State-changing entry points are memory-domain commands/handlers. Read entry points are memory-domain queries/handlers. Repositories and provider adapters are ports behind those handlers. Run orchestration may issue ingestion commands and context-pack queries; it may not write memory tables or call a retrieval provider directly.

When implemented, interfaces, types, enums, commands, queries, handlers, repositories, and errors must live in their prescribed `src/domains/memory/` subdirectories.

## Immutable accepted-result input

```ts
interface IngestAcceptedAgentResponseCommandV1 {
  command: "memory.ingest-accepted-agent-response";
  result: AcceptedAgentResponseSnapshotV1;
}

interface AcceptedAgentResponseSnapshotV1 {
  assignmentId: string;
  responseHash: string;
  acceptedResponse: AgentResponseV1;
  runId: string;
  roleId: string;
  roleKind: string;
  featureId: string;
  taskId: string | null;
  repositoryId: string;
  featurePackageHash: string;
  startingHeadOid: string;
  workspaceGeneration: number;
  observedCommitOid: string | null;
  promotedCommitOid: string | null;
  processOutcome: string;
  protocolValidationId: string;
  semanticValidationId: string;
  observedEventRange: { firstSequence: number; lastSequence: number } | null;
  receivedAt: string;
}

interface IngestAcceptedAgentResponseResultV1 {
  snapshotId: string;
  proposalIds: readonly string[];
  duplicateDelivery: boolean;
}
```

The handler receives only a response that already passed Feature 007 structural, security, path, size, and assignment-semantic validation, plus immutable Conduit-owned metadata assembled from Features 008/009. A semantic-invalid completion or secret-bearing response never reaches this command. A semantically accepted failed/blocked/partial response is permitted only under the bounded failure-lesson policy in `../spec.md`. The handler persists the response snapshot and proposal ordinals transactionally before orchestration updates the latest `result_records` read model.

Snapshot/proposal idempotency uses SHA-256 over the length-delimited UTF-8 tuple repository ID, run ID, assignment ID, response hash, and canonical unsigned decimal proposal ordinal. Each field is encoded as decimal byte length, one colon, then exact bytes. The snapshot identity omits only the final ordinal field.

## Proposal and memory records

```ts
type MemoryScopeV1 = "run" | "feature" | "project" | "global";
type MemoryTruthStateV1 =
  "active" | "stale" | "superseded" | "expired" | "tombstoned";
type MemoryProposalStateV1 = "pending" | "activated" | "rejected";

interface MemoryProposalRecordV1 {
  proposalId: string;
  snapshotId: string;
  assignmentId: string;
  responseHash: string;
  proposalOrdinal: number;
  proposedScope: "project" | "global-promotion";
  content: string;
  rationale: string;
  agentEvidenceReferences: readonly string[];
  state: MemoryProposalStateV1;
  decisionCode: string | null;
  createdAt: string;
  decidedAt: string | null;
}

interface MemoryVersionRecordV1 {
  memoryId: string;
  version: number;
  logicalPath: string;
  scope: MemoryScopeV1;
  kind: string;
  title: string;
  summary: string;
  detail: string | null;
  keywords: readonly string[];
  securityClass: string;
  truthState: MemoryTruthStateV1;
  sourceProposalIds: readonly string[];
  evidenceIds: readonly string[];
  supersedesVersion: number | null;
  featurePackageHash: string | null;
  expiresAt: string | null;
  createdAt: string;
}
```

Proposal records are immutable except for their one-way decision fields. Memory content is append-only by version. Equivalent proposals may link to one memory version; their provenance remains separate.

## Evidence

```ts
type MemoryEvidenceKindV1 =
  | "approved-contract"
  | "source-artifact"
  | "verification-observation"
  | "promotion-observation"
  | "explicit-decision"
  | "prior-memory";

interface MemoryEvidenceSnapshotV1 {
  evidenceId: string;
  kind: MemoryEvidenceKindV1;
  repositoryId: string | null;
  featurePackageHash: string | null;
  commitOid: string | null;
  relativePath: string | null;
  blobOid: string | null;
  contentHash: string | null;
  observedResultSnapshotId: string | null;
  observedEventSequences: readonly number[];
  decisionId: string | null;
  priorMemoryId: string | null;
  priorMemoryVersion: number | null;
  applicability: "current-project" | "feature" | "failure-lesson" | "global";
  capturedAt: string;
}

interface MemoryEvidenceResolutionV1 {
  evidenceId: string;
  validity: "valid" | "invalid" | "unknown";
  checkedAgainstHeadOid: string;
  checkedAgainstPackageHash: string;
  validationGeneration: string;
  sanitizedReason: string;
}

interface MemoryEvidenceResolver {
  capture(input: EvidenceCaptureInputV1): Promise<MemoryEvidenceSnapshotV1>;
  resolve(
    evidence: MemoryEvidenceSnapshotV1,
    current: EvidenceValidationContextV1,
  ): Promise<MemoryEvidenceResolutionV1>;
}
```

Evidence capture and resolution implement the commit/contract/verification/promotion validity rules in `../spec.md`. An `unknown` result is ineligible, not provisionally valid.

## Lifecycle commands

```ts
type ApprovalActorV1 =
  | { kind: "local-human"; localIdentity: string }
  | { kind: "authenticated-human"; subject: string }
  | {
      kind: "authorized-policy";
      policyId: string;
      humanAuthorizationDecisionId: string;
    };

interface EvaluatePendingMemoryProposalsCommandV1 {
  command: "memory.evaluate-pending-proposals";
  repositoryId: string;
  proposalIds: readonly string[];
  expectedTruthGeneration: string;
}

interface DecideAmbiguousMemoryProposalCommandV1 {
  command: "memory.decide-ambiguous-proposal";
  proposalId: string;
  decision: "activate" | "reject";
  actor: ApprovalActorV1;
  boundedRationale: string;
  expectedProposalState: "pending";
}

interface InvalidateMemoryCommandV1 {
  command: "memory.invalidate";
  memoryId: string;
  expectedVersion: number;
  reasonCode: string;
  evidenceGeneration: string;
}

interface TombstoneMemoryCommandV1 {
  command: "memory.tombstone";
  memoryId: string;
  expectedVersion: number;
  reasonCode: string;
}

interface PromoteGlobalMemoryCommandV1 {
  command: "memory.promote-global";
  projectMemoryId: string;
  projectMemoryVersion: number;
  promotionProposalId: string;
  actor: ApprovalActorV1;
  approvedGeneralizedContent: string;
  boundedRationale: string;
}
```

Evaluation may deterministically activate only an unambiguous project proposal fully supported by independently valid current evidence and permitted result semantics. The explicit decision command handles ambiguous project proposals; it is not required for every eligible activation. Global promotion always uses the explicit command.

A local/authenticated human invokes approval through a Conduit command or UI action. An authorized policy is valid only when its separate human authorization decision is current and covers the exact scope. An architect-role or reviewer-role `AgentResponseV1`, verdict, memory proposal, or global-promotion proposal is agent-authored input and cannot be its own approval.

All lifecycle commands use optimistic expected state/version/generation and append audit/outbox records in the same transaction.

## Context-pack query

```ts
interface CompileMemoryContextPackQueryV1 {
  query: "memory.compile-context-pack";
  assignment: AgentAssignmentV1;
  repositoryId: string;
  approvedHeadOid: string;
  featureId: string;
  featurePackageHash: string;
  contractHashes: Readonly<Record<string, string>>;
  dependencyResultSnapshotIds: readonly string[];
  memoryConfigurationHash: string;
  providerCapabilityGeneration: string;
  requiredProjectionWatermark: ProjectionWatermarkV1;
  maxQueriesPerPack: number;
  maxResultsPerQuery: number;
  maxUniqueCandidatesPerPack: number;
  contextBudgetTokens: number;
  reservedOutputTokens: number;
  tokenizerId: string;
}

interface ContextPackAuditItemV1 {
  itemId: string;
  category:
    | "assignment"
    | "role-guidance"
    | "response-contract"
    | "feature-contract"
    | "ownership-security"
    | "dependency-result"
    | "verification"
    | "project-memory"
    | "global-memory"
    | "boilerplate";
  mandatory: boolean;
  content: string;
  contentHash: string;
  memoryId: string | null;
  memoryVersion: number | null;
  evidenceIds: readonly string[];
  retrievalChannel: string;
  selectionReason: string;
  tokenCount: number;
}

interface MemoryContextPackAuditV1 {
  packId: string;
  assignmentId: string;
  snapshotId: string;
  repositoryId: string;
  approvedHeadOid: string;
  featurePackageHash: string;
  truthGeneration: string;
  evidenceGeneration: string;
  configurationGeneration: string;
  providerId: string;
  providerCapabilityGeneration: string;
  projectionWatermark: ProjectionWatermarkV1;
  retrievalMode: "configured-provider" | "turso-lexical-degraded";
  degradedReason: string | null;
  tokenizerId: string;
  budgetTokens: number;
  reservedOutputTokens: number;
  serializedInputTokens: number;
  items: readonly ContextPackAuditItemV1[];
  omitted: readonly {
    identity: string;
    reason: string;
    tokenCount: number;
  }[];
  deliveredContentHash: string;
  transientRelativePath: string;
  createdAt: string;
}

interface DeliveredMemoryContextPackV1 {
  formatVersion: "1.0";
  assignmentId: string;
  snapshotContentHash: string;
  mandatorySections: readonly {
    title: string;
    content: string;
  }[];
  optionalMemories: readonly {
    logicalPath: string;
    memoryVersion: number;
    summary: string;
    evidenceAttribution: readonly string[];
  }[];
  notices: readonly (
    | "Optional memory retrieval was degraded."
    | "Some optional memory was omitted."
  )[];
  contentHash: string;
}

interface CompiledMemoryContextPackV1 {
  audit: MemoryContextPackAuditV1;
  delivered: DeliveredMemoryContextPackV1;
}
```

The query handler coordinates canonical repositories, evidence resolution, the provider-neutral retriever, deterministic ranking, budgeting, and immutable pack persistence. Provider ID/revision, retrieval channel/scores, capability detail, degraded reason, and omission reasons exist only in `MemoryContextPackAuditV1`. Delivered Markdown is rendered only from `DeliveredMemoryContextPackV1`; its notices are deliberately provider-neutral. The query returns no database/provider handle and no secret. Mandatory overflow returns a typed failure with bounded contributor counts instead of a partial pack.

## Domain ports

```ts
interface AcceptedResponseSnapshotRepository {
  append(
    input: AcceptedAgentResponseSnapshotV1,
  ): Promise<AppendSnapshotResultV1>;
  load(
    snapshotId: string,
  ): Promise<AcceptedAgentResponseSnapshotV1 | undefined>;
}

interface MemoryProposalRepository {
  appendMany(input: readonly MemoryProposalRecordV1[]): Promise<void>;
  listPending(repositoryId: string): Promise<readonly MemoryProposalRecordV1[]>;
  decide(input: ProposalDecisionInputV1): Promise<void>;
}

interface MemoryRepository {
  appendVersion(input: MemoryVersionRecordV1): Promise<void>;
  loadVersion(
    memoryId: string,
    version: number,
  ): Promise<MemoryVersionRecordV1 | undefined>;
  loadEligibleSnapshot(
    input: EligibilitySnapshotInputV1,
  ): Promise<EligibleMemorySnapshotV1>;
}

interface MemoryProjectionOutboxRepository {
  appendInCanonicalTransaction(input: ProjectionOutboxInputV1): Promise<void>;
  claimBatch(
    input: OutboxClaimInputV1,
  ): Promise<readonly ProjectionOutboxRecordV1[]>;
  acknowledge(input: OutboxReceiptInputV1): Promise<void>;
}

interface ContextPackRepository {
  saveImmutable(pack: CompiledMemoryContextPackV1): Promise<void>;
  load(packId: string): Promise<CompiledMemoryContextPackV1 | undefined>;
}

interface MemoryContextMaterializer {
  materialize(
    delivered: DeliveredMemoryContextPackV1,
    audit: MemoryContextPackAuditV1,
    roleWorkspace: string,
  ): Promise<MaterializedPackV1>;
  remove(materialized: MaterializedPackV1): Promise<void>;
}
```

Repository implementations use Turso. The outbox worker alone calls `MemoryRetrievalProvider.apply/rebuild`. The context retriever alone calls provider search. Materialization renders only the delivered DTO and writes only the registered transient pack path after matching its content hash to the internal audit record.

## Idempotency and concurrency

- Accepted response snapshot: length-delimited SHA-256 of repository ID, run ID, assignment ID, and response hash.
- Proposal: the same length-delimited tuple plus canonical unsigned decimal proposal ordinal.
- Memory version append: `memoryId + version` with optimistic prior version.
- Truth change/outbox: one transaction and one monotonic truth sequence.
- Projection operation: stable outbox operation ID across retries.
- Pack: assignment ID plus complete snapshot/cache identity; persisted content is immutable.
- Global promotion: project memory ID/version plus promotion proposal/approval decision.

Concurrent proposal evaluation, invalidation, promotion, deletion, outbox claims, and pack compilation use optimistic versions or fenced claims. A stale writer fails rather than overwriting a later truth decision.
