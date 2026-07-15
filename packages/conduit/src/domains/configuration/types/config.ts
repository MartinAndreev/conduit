import type { AgentRoleKindValue } from "../../roles/enums/agent-role-kind.js";

export const CONFIG_FILE = "conduit.yml" as const;

export type RoleReasoningEffort =
  "minimal" | "low" | "medium" | "high" | "xhigh" | "max";

export interface RoleConfig {
  description?: string;
  runner: string;
  mode: string;
  model?: string;
  effort?: RoleReasoningEffort;
  roleKind?: AgentRoleKindValue;
  readOnly?: boolean;
  owns?: string[];
  dependsOn?: string[];
  skill: {
    source: string;
    sha256?: string;
  };
}

export interface Config {
  version: number;
  specsDir: string;
  stateDir: string;
  worktreeRoot?: string;
  worktreeRetentionDays?: number;
  runDiagnosticsRetentionDays?: number;
  roles: Record<string, RoleConfig>;
}
