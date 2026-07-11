export interface RolePortrait {
  readonly roleName: string;
  readonly label: string;
  readonly assetPath: string;
  readonly fallbackGlyph: string;
}

export interface PortraitRegistry {
  getPortrait(roleName: string): RolePortrait;
  getAllPortraits(): readonly RolePortrait[];
  overrideAssetPath(roleName: string, assetPath: string): void;
}
