export type RunStatus =
  | "planned"
  | "running"
  | "completed"
  | "failed"
  | "cancelled"
  | "awaiting-input";

export type TerminalRunStatus = "completed" | "failed" | "cancelled";

export type RunFailureKind =
  | "missing-response"
  | "structural-response"
  | "semantic-response"
  | "reported-noncompletion"
  | "runner"
  | "policy";

export interface ReviewerWorkflowState {
  correctionRound: number;
  findingFingerprints: string[];
}

export interface RunRole {
  name: string;
  runner: string;
  model?: string;
  effort?: import("../../configuration/types/config.js").RoleReasoningEffort;
  readOnly: boolean;
  owns: string[];
  dependsOn: string[];
  promptFile: string;
  prompt: string;
  context?: string;
  contextFile?: string;
  command: string;
  args: string[];
  skillSource: string;
  status: RunStatus;
  worktree?: string;
  worktreeHead?: string;
  diffBaselineHead?: string;
  workspaceRepositoryId?: string;
  workspaceRoleKey?: string;
  workspaceBranchName?: string;
  workspaceAssignmentHash?: string;
  workspaceLeaseOwner?: string;
  workspaceFencingToken?: number;
  linkedWorkspacePaths?: string[];
  integrationCommits?: string[];
  pendingResumeCommits?: string[];
  resumeObservedFiles?: string[];
  resumeAttempt?: number;
  lastFailureKind?: RunFailureKind;
  worktreePromptFile?: string;
  finalOutputFile?: string;
  assignment?: import("./agent-protocol.js").AgentAssignmentV1;
}

export interface Run {
  id: string;
  featureId: string;
  status: RunStatus;
  createdAt: string;
  roles: RunRole[];
  startingHead?: string;
  featurePackageHash?: string;
  featurePackagePath?: string;
  reviewerWorkflow?: ReviewerWorkflowState;
  stateDirectory?: string;
  worktreeRoot?: string;
  worktreeRetentionDays?: number;
  runDiagnosticsRetentionDays?: number;
}

export interface RunResult {
  role: string;
  status: "completed" | "failed" | "cancelled" | "dry-run";
  exitCode?: number;
  error?: string;
  output?: string;
  stdout?: string;
  files?: string[];
  command?: string[];
  resultRecord?: import("./agent-protocol.js").ConduitResultRecordV1;
  retryable?: boolean;
  failureKind?: RunFailureKind;
}
