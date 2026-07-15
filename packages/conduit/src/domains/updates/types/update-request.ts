import type { InstallationStrategy } from "./installation.js";
import type { StableRelease } from "./release.js";

export interface UpdateRequest {
  readonly currentVersion: string;
  readonly release: StableRelease;
  readonly installation: InstallationStrategy;
}
