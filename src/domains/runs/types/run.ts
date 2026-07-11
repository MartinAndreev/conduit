export type RunStatus =
  | "planned"
  | "running"
  | "completed"
  | "failed"
  | "cancelled"
  | "awaiting-input";

export interface RunRole {
  name: string;
  runner: string;
  readOnly: boolean;
  owns: string[];
  promptFile: string;
  prompt: string;
  command: string;
  args: string[];
  skillSource: string;
  status: RunStatus;
  worktree?: string;
  worktreePromptFile?: string;
}

export interface Run {
  id: string;
  featureId: string;
  status: RunStatus;
  createdAt: string;
  roles: RunRole[];
}

export interface RunResult {
  role: string;
  status: "completed" | "failed" | "cancelled" | "dry-run";
  exitCode?: number;
  error?: string;
  output?: string;
  files?: string[];
  command?: string[];
}
