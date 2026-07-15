export type AgentStatus =
  | "completed"
  | "partial"
  | "blocked"
  | "needs_input"
  | "failed";

export type AgentVerdictDecision =
  | "approved"
  | "rejected"
  | "passed"
  | "failed"
  | "needs_changes"
  | "inconclusive";

export type AgentRoleKind =
  | "implementation"
  | "reviewer"
  | "research"
  | "architect"
  | "qa"
  | "documentation"
  | "custom";

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
  readonly requiredVerification?: readonly string[];
  readonly expectedArtifacts?: readonly string[];
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
