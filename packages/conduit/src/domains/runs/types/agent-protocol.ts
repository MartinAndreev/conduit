export type AgentStatus =
  "completed" | "partial" | "blocked" | "needs_input" | "failed";

export type AgentVerdictDecision =
  | "approved"
  | "rejected"
  | "passed"
  | "failed"
  | "needs_changes"
  | "inconclusive";

import type { AgentRoleKindValue } from "../../roles/enums/agent-role-kind.js";

export type AgentRoleKind = AgentRoleKindValue;

export interface AgentAssignmentSecurityV1 {
  readonly databaseEnvironmentRemoved: true;
  readonly databaseAccessForbidden: true;
  readonly memoryActivationForbidden: true;
  readonly secretReportingForbidden: true;
}

export interface AgentAssignmentSizeLimitsV1 {
  readonly responseBytes: number;
  readonly summaryCharacters: number;
  readonly collectionItems: number;
}

export interface AgentAssignmentV1 {
  readonly assignmentId: string;
  readonly role: string;
  readonly roleKind: AgentRoleKind;
  readonly objective: string;
  readonly ownedPaths: readonly string[];
  readonly forbiddenPaths: readonly string[];
  readonly dependencies: readonly string[];
  readonly contextReferences: readonly string[];
  readonly acceptanceCriteria: readonly string[];
  readonly contracts: readonly string[];
  readonly requiredVerification: readonly string[];
  readonly expectedCapabilities: readonly string[];
  readonly requiredResponseFields: readonly string[];
  readonly outputSchemaRef: "agent-response-v1.schema.json";
  readonly security: AgentAssignmentSecurityV1;
  readonly sizeLimits: AgentAssignmentSizeLimitsV1;
}

export interface AgentVerdictV1 {
  readonly decision: AgentVerdictDecision;
  readonly rationale: string;
}

export interface AgentArtifactV1 {
  readonly path: string;
  readonly category: string;
  readonly purpose: string;
  readonly action: "created" | "modified" | "deleted" | "inspected";
}

export interface AgentFindingV1 {
  readonly severity: "info" | "warning" | "error" | "critical";
  readonly category: string;
  readonly message: string;
  readonly path?: string;
  readonly line?: number;
  readonly evidence: readonly string[];
  readonly suggestedRemediation?: string;
}

export interface AgentVerificationV1 {
  readonly operation: string;
  readonly outcome: "passed" | "failed" | "skipped" | "blocked" | "unknown";
  readonly exitCode?: number;
  readonly summary: string;
  readonly evidence?: readonly string[];
}

export interface AgentDecisionV1 {
  readonly decision: string;
  readonly rationale: string;
  readonly affectedPaths?: readonly string[];
}

export interface AgentBlockerV1 {
  readonly blocker: string;
  readonly impact: string;
  readonly minimumUnblocker: string;
}

export interface AgentQuestionV1 {
  readonly question: string;
  readonly whyItMatters: string;
  readonly context: string;
  readonly options: readonly string[];
  readonly smallestUnblocker: string;
}

export interface AgentRiskV1 {
  readonly risk: string;
  readonly category:
    | "technical"
    | "integration"
    | "security"
    | "compatibility"
    | "verification"
    | "operational"
    | "other";
  readonly mitigation: string;
}

export interface AgentEvidenceV1 {
  readonly kind:
    | "path"
    | "line"
    | "symbol"
    | "contract"
    | "command"
    | "url"
    | "runner_event"
    | "other";
  readonly reference: string;
  readonly summary?: string;
}

export interface AgentMemoryProposalV1 {
  readonly scope: "project";
  readonly content: string;
  readonly rationale: string;
  readonly evidence?: readonly string[];
}

export interface AgentGlobalPromotionProposalV1 {
  readonly content: string;
  readonly rationale: string;
  readonly evidence: readonly string[];
}

export interface AgentResponseV1 {
  readonly protocolVersion: "1.0";
  readonly status: AgentStatus;
  readonly summary: string;
  readonly verdict: AgentVerdictV1 | null;
  readonly artifacts: readonly AgentArtifactV1[];
  readonly findings: readonly AgentFindingV1[];
  readonly verification: readonly AgentVerificationV1[];
  readonly decisions: readonly AgentDecisionV1[];
  readonly blockers: readonly AgentBlockerV1[];
  readonly questions: readonly AgentQuestionV1[];
  readonly risks: readonly AgentRiskV1[];
  readonly evidence: readonly AgentEvidenceV1[];
  readonly memoryProposals: readonly AgentMemoryProposalV1[];
  readonly globalPromotionProposals: readonly AgentGlobalPromotionProposalV1[];
}

export interface AgentAssignmentPolicyV1 {
  readonly roleKind: AgentRoleKind;
  readonly ownedPaths: readonly string[];
  readonly forbiddenPaths?: readonly string[];
  readonly requiredVerification?: readonly string[];
  readonly expectedArtifacts?: readonly string[];
  readonly observedChangedFiles?: readonly string[];
  readonly readOnly?: boolean;
}

export interface AgentProcessResultV1 {
  readonly exitCode: number;
  readonly acceptable: boolean;
  readonly cancelled: boolean;
}

export interface AgentValidationMetadataV1 {
  readonly valid: boolean;
  readonly issues: readonly ValidationIssue[];
}

export interface ConduitResultRecordV1 {
  readonly recordVersion: "1.0";
  readonly runId: string;
  readonly featureId: string;
  readonly taskId: string | null;
  readonly assignmentId: string;
  readonly role: string;
  readonly runner: string;
  readonly model: string | null;
  readonly receivedAt: string;
  readonly process: AgentProcessResultV1;
  readonly observedChangedFiles: readonly string[];
  readonly conduitObservedEvents: readonly import("./runner-events.js").RunnerEvent[];
  readonly runnerReportedEvents: readonly import("./runner-events.js").RunnerEvent[];
  readonly agentClaimedEvents: readonly import("./runner-events.js").RunnerEvent[];
  readonly protocolValidation: AgentValidationMetadataV1;
  readonly semanticValidation: AgentValidationMetadataV1;
  readonly ownershipWarnings?: readonly ValidationIssue[];
  readonly response: AgentResponseV1;
}

export interface ValidationIssue {
  readonly path: string;
  readonly message: string;
}

export interface ValidationResult<T> {
  readonly valid: boolean;
  readonly value?: T;
  readonly issues: readonly ValidationIssue[];
}
