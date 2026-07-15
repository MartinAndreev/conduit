export type RunStatus =
  | "planned"
  | "running"
  | "completed"
  | "failed"
  | "cancelled"
  | "awaiting-input";

export type TerminalRunStatus = "completed" | "failed" | "cancelled";

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
}
