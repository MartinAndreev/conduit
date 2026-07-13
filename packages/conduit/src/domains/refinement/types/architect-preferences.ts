export const ARCHITECT_EFFORTS = [
  "standard",
  "thorough",
  "exhaustive",
] as const;
export type ArchitectEffort = (typeof ARCHITECT_EFFORTS)[number];

export const ARCHITECT_DETAIL_LEVELS = [
  "concise",
  "implementation-ready",
  "implementation-blueprint",
] as const;
export type ArchitectDetailLevel = (typeof ARCHITECT_DETAIL_LEVELS)[number];

export interface ArchitectPreferences {
  readonly effort: ArchitectEffort;
  readonly detailLevel: ArchitectDetailLevel;
}

export const DEFAULT_ARCHITECT_PREFERENCES: ArchitectPreferences = {
  effort: "thorough",
  detailLevel: "implementation-blueprint",
};
