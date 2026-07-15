import { constants } from "node:fs";
import { access, realpath } from "node:fs/promises";
import path from "node:path";
import { conduitStandaloneBuild } from "../../../generated/build-context.js";
import { InstallationKind } from "../enums/installation-kind.js";
import { releaseAssetName } from "../helpers/platform-asset.js";
import type { InstallationDetector } from "../interfaces/installation-detector.js";
import type { InstallationDetectionContext } from "../types/installation-detection-context.js";
import type { InstallationStrategy } from "../types/installation.js";

const RELEASES_URL = "https://github.com/MartinAndreev/conduit/releases/latest";

function normalized(value: string): string {
  return value.replaceAll("\\", "/").toLowerCase();
}

function packageManagerFromPath(
  entryPath: string,
): "npm" | "pnpm" | "bun" | undefined {
  const value = normalized(entryPath);
  if (value.includes("/.bun/install/global/node_modules/conduit-orchestrator/"))
    return "bun";
  if (
    value.includes("/pnpm/global/") &&
    value.includes("/conduit-orchestrator@")
  )
    return "pnpm";
  if (
    (value.includes("/lib/node_modules/conduit-orchestrator/") ||
      value.includes("/npm/node_modules/conduit-orchestrator/")) &&
    !value.includes("/.pnpm/")
  )
    return "npm";
  return undefined;
}

function packageCommand(manager: "npm" | "pnpm" | "bun", version: string) {
  const target = `conduit-orchestrator@${version}`;
  if (manager === "npm") return `npm install --global ${target}`;
  if (manager === "pnpm") return `pnpm add --global ${target}`;
  return `bun add --global ${target}`;
}

export class DefaultInstallationDetector implements InstallationDetector {
  constructor(
    private readonly context: InstallationDetectionContext = {
      standaloneBuild: conduitStandaloneBuild,
      executablePath: process.execPath,
      entryPath: process.argv[1],
      platform: process.platform,
      architecture: process.arch,
    },
  ) {}

  async detect(): Promise<InstallationStrategy> {
    const assetName = releaseAssetName(
      this.context.platform,
      this.context.architecture,
    );
    if (this.context.standaloneBuild) {
      if (!assetName)
        return {
          kind: InstallationKind.Unsupported,
          automatic: false,
          label: "Unsupported standalone platform",
          manualUrl: RELEASES_URL,
          reason: "No official release asset exists for this platform.",
        };
      if (this.context.platform === "win32")
        return {
          kind: InstallationKind.Unsupported,
          automatic: false,
          label: "Windows standalone (manual update required)",
          manualUrl: RELEASES_URL,
          reason: "Deferred replacement is not enabled on Windows.",
        };
      try {
        await access(path.dirname(this.context.executablePath), constants.W_OK);
      } catch {
        return {
          kind: InstallationKind.Unsupported,
          automatic: false,
          label: "Read-only standalone installation",
          manualUrl: RELEASES_URL,
          reason: "The executable directory is not writable.",
        };
      }
      return {
        kind: InstallationKind.Standalone,
        automatic: true,
        label: "Official standalone binary",
        executablePath: this.context.executablePath,
        assetName,
      };
    }

    const entryPath = this.context.entryPath
      ? await realpath(this.context.entryPath).catch(
          () => this.context.entryPath,
        )
      : undefined;
    if (!entryPath)
      return {
        kind: InstallationKind.Unknown,
        automatic: false,
        label: "Unknown installation (manual update only)",
        manualUrl: RELEASES_URL,
        reason: "The executable entry path is unavailable.",
      };
    const manager = packageManagerFromPath(entryPath);
    if (manager)
      return {
        kind: InstallationKind.GlobalPackage,
        automatic: true,
        label: `${manager} global package`,
        packageManager: manager,
        manualCommand: packageCommand(manager, "<version>"),
      };

    const value = normalized(entryPath);
    if (value.includes("/node_modules/conduit-orchestrator/"))
      return {
        kind: InstallationKind.LocalDependency,
        automatic: false,
        label: "Project-local dependency",
        manualCommand: "Update conduit-orchestrator in the owning project.",
        reason: "Conduit will not modify a project manifest or lockfile.",
      };
    if (
      value.includes("/packages/conduit/") ||
      value.endsWith("/bin/conduit.js")
    )
      return {
        kind: InstallationKind.SourceCheckout,
        automatic: false,
        label: "Source checkout",
        manualCommand: "Pull and rebuild the source checkout.",
        reason: "Source checkouts are not self-updated.",
      };
    return {
      kind: InstallationKind.Unknown,
      automatic: false,
      label: "Unknown installation (manual update only)",
      manualUrl: RELEASES_URL,
      reason: "No supported installation method was positively identified.",
    };
  }
}
