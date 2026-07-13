export const CONFIG_FILE = "conduit.yml" as const;

export type RoleReasoningEffort =
  "minimal" | "low" | "medium" | "high" | "xhigh" | "max";

export interface RoleConfig {
  description?: string;
  runner: string;
  mode: string;
  model?: string;
  effort?: RoleReasoningEffort;
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
  roles: Record<string, RoleConfig>;
}
