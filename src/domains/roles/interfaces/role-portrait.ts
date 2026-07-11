/** Render-ready description of a configured role portrait. */
export interface RolePortrait {
  readonly roleName: string;
  readonly label: string;
  readonly assetPath: string;
  readonly fallbackGlyph: string;
  /** Ordered fixed-width frames used for the role's terminal animation. */
  readonly frames: readonly (readonly string[])[];
}
