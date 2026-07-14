export interface GlobalSettings {
  readonly version: number;
  readonly defaultProvider: string;
  readonly credentialProfiles: Readonly<Record<string, CredentialProfileRef>>;
  readonly providerSettings: Readonly<Record<string, ProviderSettings>>;
}

export interface CredentialProfileRef {
  readonly name: string;
  readonly description?: string;
}

export interface ProviderSettings {
  readonly enabled: boolean;
  readonly options: Readonly<Record<string, string | number | boolean>>;
}

export interface ProjectSettings {
  readonly provider: string;
  readonly credentialProfile?: string;
  readonly specsDir: string;
  readonly stateDir: string;
  readonly providerOptions: Readonly<Record<string, string | number | boolean>>;
}

export interface ResolvedSettings {
  readonly global: GlobalSettings;
  readonly globalProfile: GlobalProfile | undefined;
  readonly project: ProjectSettings | undefined;
  readonly effective: EffectiveSettings;
  readonly roles: Readonly<Record<string, ResolvedRoleSettings>>;
  readonly provenance: Readonly<Record<string, ConfigurationSource>>;
}

export interface ResolvedRoleSettings {
  readonly runner: string;
  readonly model?: string;
  readonly effort?: RoleReasoningEffort;
  readonly mode: string;
  readonly readOnly: boolean;
  readonly owns: readonly string[];
  readonly skillSource: string;
  readonly guidance?: string;
}

export interface EffectiveSettings {
  readonly provider: string;
  readonly credentialProfile: string | undefined;
  readonly specsDir: string;
  readonly stateDir: string;
  readonly providerOptions: Readonly<Record<string, string | number | boolean>>;
}

export const DEFAULT_GLOBAL_SETTINGS: GlobalSettings = {
  version: 1,
  defaultProvider: "local-spec-kit",
  credentialProfiles: {},
  providerSettings: {},
};

export const DEFAULT_PROJECT_SETTINGS: ProjectSettings = {
  provider: "local-spec-kit",
  specsDir: "specs",
  stateDir: ".conduit",
  providerOptions: {},
};
import type { RoleReasoningEffort } from "./config.js";
import type { GlobalProfile } from "./global-profile.js";

export type ConfigurationSource =
  "builtin" | "global-profile" | "project" | "role-guidance" | "cli";
