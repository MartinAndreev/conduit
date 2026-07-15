import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { InstallationKind } from "../enums/installation-kind.js";
import { UpdateProgressStage } from "../enums/update-progress-stage.js";
import {
  UpdateProcessError,
  UpdateValidationError,
} from "../errors/update-errors.js";
import { sanitizeProcessDiagnostic } from "../helpers/diagnostic-sanitizer.js";
import { parseSemanticVersion } from "../helpers/semver.js";
import type { ProcessExecutor } from "../interfaces/process-executor.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import { NodeProcessExecutor } from "./node-process-executor.js";

const MAXIMUM_OUTPUT_BYTES = 16 * 1024;
const PROCESS_TIMEOUT_MS = 120_000;

function commandArguments(
  manager: "npm" | "pnpm" | "bun",
  target: string,
): readonly string[] {
  if (manager === "npm") return ["install", "--global", target];
  return ["add", "--global", target];
}

export class PackageUpdateInstaller implements UpdateInstaller {
  constructor(
    private readonly processExecutor: ProcessExecutor = new NodeProcessExecutor(),
  ) {}

  async install(
    request: Parameters<UpdateInstaller["install"]>[0],
    onProgress: Parameters<UpdateInstaller["install"]>[1],
  ): Promise<void> {
    const { installation, release } = request;
    if (
      installation.kind !== InstallationKind.GlobalPackage ||
      !installation.packageManager
    )
      throw new UpdateValidationError(
        "INVALID_PACKAGE_STRATEGY",
        "The package installation strategy is invalid.",
      );
    const version = parseSemanticVersion(release.version);
    if (!version || version.prerelease.length > 0)
      throw new UpdateValidationError(
        "INVALID_TARGET_VERSION",
        "The target package version is invalid.",
      );

    const target = `conduit-orchestrator@${release.version}`;
    const directory = await mkdtemp(path.join(tmpdir(), "conduit-update-"));
    try {
      onProgress({
        stage: UpdateProgressStage.Preparing,
        message: `Preparing ${installation.packageManager} update.`,
      });
      onProgress({
        stage: UpdateProgressStage.Installing,
        message: `Installing conduit-orchestrator ${release.version}.`,
      });
      const result = await this.processExecutor.execute({
        executable: installation.packageManager,
        arguments: commandArguments(installation.packageManager, target),
        cwd: directory,
        timeoutMs: PROCESS_TIMEOUT_MS,
        maximumOutputBytes: MAXIMUM_OUTPUT_BYTES,
      });
      if (result.timedOut)
        throw new UpdateProcessError(
          "PACKAGE_TIMEOUT",
          "The package-manager update timed out.",
        );
      if (result.exitCode !== 0) {
        const diagnostic = sanitizeProcessDiagnostic(
          result.stderr || result.stdout,
        );
        throw new UpdateProcessError(
          "PACKAGE_FAILED",
          diagnostic
            ? `The package manager failed: ${diagnostic}`
            : "The package manager failed without a diagnostic.",
        );
      }
      onProgress({
        stage: UpdateProgressStage.Complete,
        message: "The package update is ready for the next launch.",
      });
    } catch (cause) {
      if (
        cause instanceof UpdateProcessError ||
        cause instanceof UpdateValidationError
      )
        throw cause;
      throw new UpdateProcessError(
        "PACKAGE_LAUNCH_FAILED",
        "The package manager could not be started.",
        true,
        cause,
      );
    } finally {
      await rm(directory, { recursive: true, force: true }).catch(
        () => undefined,
      );
    }
  }
}
