import type { InstallationStrategy } from "../types/installation.js";

export interface InstallationDetector {
  detect(): Promise<InstallationStrategy>;
}
