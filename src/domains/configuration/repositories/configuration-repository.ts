import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import os from "node:os";
import { pathExists } from "../../../config.js";
import type { Config } from "../types/config.js";
import type {
  GlobalSettings,
  ProjectSettings,
  ResolvedSettings,
  EffectiveSettings,
} from "../types/settings.js";
import {
  DEFAULT_GLOBAL_SETTINGS,
  DEFAULT_PROJECT_SETTINGS,
} from "../types/settings.js";

const GLOBAL_CONFIG_DIR_NAME = "conduit";
const GLOBAL_SETTINGS_FILE = "settings.yml";

function globalConfigDir(): string {
  const platform = process.platform;
  const home = os.homedir();
  if (platform === "darwin") {
    return path.join(
      home,
      "Library",
      "Application Support",
      GLOBAL_CONFIG_DIR_NAME,
    );
  }
  if (platform === "win32") {
    const appData =
      process.env.APPDATA ?? path.join(home, "AppData", "Roaming");
    return path.join(appData, GLOBAL_CONFIG_DIR_NAME);
  }
  return path.join(home, ".config", GLOBAL_CONFIG_DIR_NAME);
}

function serializeGlobalSettings(settings: GlobalSettings): string {
  const lines: string[] = [
    `version: ${settings.version}`,
    `defaultProvider: ${settings.defaultProvider}`,
  ];
  if (Object.keys(settings.credentialProfiles).length > 0) {
    lines.push("credentialProfiles:");
    for (const [name, ref] of Object.entries(settings.credentialProfiles)) {
      lines.push(`  ${name}:`);
      if (ref.description) lines.push(`    description: "${ref.description}"`);
    }
  }
  if (Object.keys(settings.providerSettings).length > 0) {
    lines.push("providerSettings:");
    for (const [name, ps] of Object.entries(settings.providerSettings)) {
      lines.push(`  ${name}:`);
      lines.push(`    enabled: ${ps.enabled}`);
      if (Object.keys(ps.options).length > 0) {
        lines.push("    options:");
        for (const [k, v] of Object.entries(ps.options)) {
          lines.push(`      ${k}: ${v}`);
        }
      }
    }
  }
  return lines.join("\n") + "\n";
}

function parseGlobalSettings(yaml: string): GlobalSettings {
  const settings = {
    ...DEFAULT_GLOBAL_SETTINGS,
    credentialProfiles: {} as Record<
      string,
      { name: string; description?: string }
    >,
    providerSettings: {} as Record<
      string,
      { enabled: boolean; options: Record<string, string | number | boolean> }
    >,
  };
  let section: string | undefined;
  let subKey: string | undefined;

  for (const rawLine of yaml.split(/\r?\n/)) {
    if (!rawLine.trim() || rawLine.trimStart().startsWith("#")) continue;
    const indent = rawLine.length - rawLine.trimStart().length;
    const line = rawLine.trim();
    const match = line.match(/^([\w-]+):(?:\s*(.*))?$/);
    if (!match) continue;
    const [, key, rawValue] = match;

    if (indent === 0) {
      section = key;
      subKey = undefined;
      if (key === "version" && rawValue) settings.version = Number(rawValue);
      if (key === "defaultProvider" && rawValue)
        settings.defaultProvider = rawValue.replace(/^['"]|['"]$/g, "");
    } else if (indent === 2) {
      subKey = key;
      if (section === "credentialProfiles") {
        settings.credentialProfiles[key] = { name: key };
      } else if (section === "providerSettings") {
        settings.providerSettings[key] = { enabled: true, options: {} };
      }
    } else if (indent === 4 && subKey) {
      const value = rawValue?.replace(/^['"]|['"]$/g, "") ?? "";
      if (
        section === "credentialProfiles" &&
        settings.credentialProfiles[subKey]
      ) {
        if (key === "description")
          settings.credentialProfiles[subKey] = {
            ...settings.credentialProfiles[subKey],
            description: value,
          };
      } else if (
        section === "providerSettings" &&
        settings.providerSettings[subKey]
      ) {
        if (key === "enabled")
          settings.providerSettings[subKey] = {
            ...settings.providerSettings[subKey],
            enabled: value === "true",
          };
      }
    }
  }
  return settings as GlobalSettings;
}

function parseProjectSettings(config: Config): ProjectSettings {
  return {
    provider: DEFAULT_PROJECT_SETTINGS.provider,
    specsDir: config.specsDir,
    stateDir: config.stateDir,
    providerOptions: {},
  };
}

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

export function createConfigurationRepository(): ConfigurationRepository {
  const gDir = globalConfigDir();

  return {
    getGlobalConfigDir(): string {
      return gDir;
    },

    async loadGlobalSettings(): Promise<GlobalSettings> {
      const settingsPath = path.join(gDir, GLOBAL_SETTINGS_FILE);
      if (!(await pathExists(settingsPath))) {
        return DEFAULT_GLOBAL_SETTINGS;
      }
      try {
        const content = await readFile(settingsPath, "utf8");
        return parseGlobalSettings(content);
      } catch {
        return DEFAULT_GLOBAL_SETTINGS;
      }
    },

    async saveGlobalSettings(settings: GlobalSettings): Promise<void> {
      await mkdir(gDir, { recursive: true });
      const settingsPath = path.join(gDir, GLOBAL_SETTINGS_FILE);
      await writeFile(settingsPath, serializeGlobalSettings(settings), {
        mode: 0o600,
      });
    },

    async loadProjectConfig(projectRoot: string): Promise<Config> {
      const { loadConfig } = await import("../../../config.js");
      return loadConfig(projectRoot);
    },

    async resolveSettings(
      projectRoot: string,
      cliOptions?: Record<string, unknown>,
    ): Promise<ResolvedSettings> {
      const global = await this.loadGlobalSettings();
      let project: ProjectSettings | undefined;

      try {
        const config = await this.loadProjectConfig(projectRoot);
        project = parseProjectSettings(config);
      } catch {
        // No project config
      }

      const effective: EffectiveSettings = {
        provider:
          (cliOptions?.provider as string) ??
          project?.provider ??
          global.defaultProvider,
        credentialProfile:
          (cliOptions?.credentialProfile as string) ??
          project?.credentialProfile,
        specsDir:
          (cliOptions?.specsDir as string) ??
          project?.specsDir ??
          DEFAULT_PROJECT_SETTINGS.specsDir,
        stateDir:
          (cliOptions?.stateDir as string) ??
          project?.stateDir ??
          DEFAULT_PROJECT_SETTINGS.stateDir,
        providerOptions: {
          ...global.providerSettings[
            project?.provider ?? global.defaultProvider
          ]?.options,
          ...project?.providerOptions,
        },
      };

      return { global, project, effective };
    },
  };
}
