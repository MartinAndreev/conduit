import { InstallationKind } from "../enums/installation-kind.js";
import type { InstallationDetector } from "../interfaces/installation-detector.js";
import type { InstallationStrategy } from "../types/installation.js";

export class UnknownInstallationDetector implements InstallationDetector {
  async detect(): Promise<InstallationStrategy> {
    return {
      kind: InstallationKind.Unknown,
      automatic: false,
      label: "Unknown installation (manual update only)",
      manualUrl: "https://github.com/MartinAndreev/conduit/releases/latest",
      reason: "Automatic installation detection is unavailable.",
    };
  }
}
