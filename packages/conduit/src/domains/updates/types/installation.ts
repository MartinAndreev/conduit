import type { InstallationKind } from "../enums/installation-kind.js";

export interface InstallationStrategy {
  readonly kind: InstallationKind;
  readonly automatic: boolean;
  readonly label: string;
  readonly executablePath?: string;
  readonly assetName?: string;
  readonly packageManager?: "npm" | "pnpm" | "bun";
  readonly manualCommand?: string;
  readonly manualUrl?: string;
  readonly reason?: string;
}
