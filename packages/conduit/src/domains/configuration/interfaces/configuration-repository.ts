import type { Config } from "../types/config.js";
import type { GlobalSettings, ResolvedSettings } from "../types/settings.js";

export interface ConfigurationRepository {
  loadGlobalSettings(): Promise<GlobalSettings>;
  saveGlobalSettings(settings: GlobalSettings): Promise<void>;
  loadProjectConfig(projectRoot: string): Promise<Config>;
  resolveSettings(
    projectRoot: string,
    cliOptions?: Record<string, unknown>,
  ): Promise<ResolvedSettings>;
  getGlobalConfigDir(): string;
}
