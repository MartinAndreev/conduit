import type { RolePortrait } from "./role-portrait.js";

export interface PortraitRegistry {
  getPortrait(roleName: string): RolePortrait;
  getAllPortraits(): readonly RolePortrait[];
  overrideAssetPath(roleName: string, assetPath: string): void;
}
