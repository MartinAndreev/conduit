import type { StableRelease } from "../types/release.js";

export interface ReleaseDiscovery {
  discover(currentVersion: string): Promise<StableRelease | undefined>;
}
