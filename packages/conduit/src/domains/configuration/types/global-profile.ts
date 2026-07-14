import type { RoleReasoningEffort } from "./config.js";

export type GlobalProfile = Readonly<{
  name: string;
  runner?: string;
  model?: string;
  effort?: RoleReasoningEffort;
  mode?: string;
  readOnly?: boolean;
  owns: readonly string[];
  skillSource?: string;
  metadata: Readonly<Record<string, string>>;
  version: number;
  createdAt: string;
  updatedAt: string;
}>;

export type SaveGlobalProfileInput = Readonly<{
  name: string;
  runner?: string;
  model?: string;
  effort?: RoleReasoningEffort;
  mode?: string;
  readOnly?: boolean;
  owns?: readonly string[];
  skillSource?: string;
  metadata?: Readonly<Record<string, string>>;
  expectedVersion?: number;
}>;
