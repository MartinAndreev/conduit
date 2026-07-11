export const CONFIG_FILE = "conduit.yml" as const;

export interface RoleConfig {
  description?: string;
  runner: string;
  mode: string;
  model?: string;
  readOnly?: boolean;
  owns?: string[];
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
