import { constants } from "node:fs";
import {
  access,
  chmod,
  mkdtemp,
  open,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import { InstallationKind } from "../enums/installation-kind.js";
import { UpdateProgressStage } from "../enums/update-progress-stage.js";
import {
  UpdateIntegrityError,
  UpdatePermissionError,
  UpdateReplacementError,
  UpdateValidationError,
} from "../errors/update-errors.js";
import { expectedSha256, sha256 } from "../helpers/checksum.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import type { ExecutableReplacer } from "../interfaces/executable-replacer.js";
import type { ReleaseAsset } from "../types/release.js";
import type { UpdateProgressEvent } from "../types/update-progress.js";
import type { UpdateRequest } from "../types/update-request.js";
import { ReleaseAssetFetcher } from "./release-asset-fetcher.js";
import { AtomicExecutableReplacer } from "./atomic-executable-replacer.js";

const CHECKSUM_ASSET = "SHA256SUMS";
const MAXIMUM_CHECKSUM_BYTES = 1024 * 1024;
const MAXIMUM_BINARY_BYTES = 256 * 1024 * 1024;

function exactAsset(
  assets: readonly ReleaseAsset[],
  name: string,
): ReleaseAsset {
  const matches = assets.filter((asset) => asset.name === name);
  if (matches.length !== 1)
    throw new UpdateValidationError(
      matches.length === 0 ? "MISSING_ASSET" : "DUPLICATE_ASSET",
      matches.length === 0
        ? `The expected ${name} release asset is missing.`
        : `The expected ${name} release asset is duplicated.`,
    );
  return matches[0]!;
}

function validateOfficialDownload(
  asset: ReleaseAsset,
  name: string,
  tagName: string,
): void {
  const url = new URL(asset.url);
  const expectedPrefix = `/MartinAndreev/conduit/releases/download/${encodeURIComponent(tagName)}/`;
  if (
    url.hostname !== "github.com" ||
    !url.pathname.startsWith(expectedPrefix) ||
    !url.pathname.endsWith(`/${encodeURIComponent(name)}`)
  )
    throw new UpdateValidationError(
      "WRONG_RELEASE_ASSET",
      `The ${name} asset does not belong to the official release.`,
    );
}

export class VerifiedStandaloneInstaller implements UpdateInstaller {
  constructor(
    private readonly assetFetcher: ReleaseAssetFetcher = new ReleaseAssetFetcher(),
    private readonly executableReplacer: ExecutableReplacer = new AtomicExecutableReplacer(),
  ) {}

  async install(
    request: UpdateRequest,
    onProgress: (event: UpdateProgressEvent) => void,
  ): Promise<void> {
    const { installation, release } = request;
    if (
      installation.kind !== InstallationKind.Standalone ||
      !installation.automatic ||
      !installation.executablePath ||
      !installation.assetName ||
      process.platform === "win32"
    )
      throw new UpdateValidationError(
        "INVALID_STANDALONE_STRATEGY",
        "Automatic standalone replacement is unavailable.",
      );
    const binaryAsset = exactAsset(release.assets, installation.assetName);
    const checksumAsset = exactAsset(release.assets, CHECKSUM_ASSET);
    validateOfficialDownload(
      binaryAsset,
      installation.assetName,
      release.tagName,
    );
    validateOfficialDownload(checksumAsset, CHECKSUM_ASSET, release.tagName);
    if (binaryAsset.size <= 0 || binaryAsset.size > MAXIMUM_BINARY_BYTES)
      throw new UpdateValidationError(
        "INVALID_ASSET_SIZE",
        "The standalone release asset size is invalid.",
      );

    const destination = installation.executablePath;
    const destinationDirectory = path.dirname(destination);
    try {
      await access(destinationDirectory, constants.W_OK);
    } catch (cause) {
      throw new UpdatePermissionError(
        "DESTINATION_READ_ONLY",
        "The Conduit installation directory is not writable.",
        cause,
      );
    }

    onProgress({
      stage: UpdateProgressStage.Preparing,
      message: "Preparing a staged standalone update.",
    });
    const temporaryDirectory = await mkdtemp(
      path.join(destinationDirectory, ".conduit-update-"),
    );
    const stagedExecutable = path.join(
      temporaryDirectory,
      installation.assetName,
    );
    let replaced = false;
    try {
      onProgress({
        stage: UpdateProgressStage.Downloading,
        message: `Downloading Conduit ${release.version}.`,
      });
      const [binary, checksumBytes] = await Promise.all([
        this.assetFetcher.fetch(
          binaryAsset.url,
          Math.min(MAXIMUM_BINARY_BYTES, binaryAsset.size),
        ),
        this.assetFetcher.fetch(checksumAsset.url, MAXIMUM_CHECKSUM_BYTES),
      ]);
      if (binary.byteLength !== binaryAsset.size)
        throw new UpdateIntegrityError(
          "ASSET_SIZE_MISMATCH",
          "The downloaded release asset size does not match its metadata.",
        );
      onProgress({
        stage: UpdateProgressStage.Verifying,
        message: "Verifying the standalone checksum.",
      });
      const expected = expectedSha256(
        new TextDecoder().decode(checksumBytes),
        installation.assetName,
      );
      if (sha256(binary) !== expected)
        throw new UpdateIntegrityError(
          "CHECKSUM_MISMATCH",
          "The standalone release checksum does not match.",
        );

      const current = await stat(destination);
      await writeFile(stagedExecutable, binary, {
        flag: "wx",
        mode: current.mode & 0o777,
      });
      await chmod(stagedExecutable, current.mode & 0o777);
      const stagedHandle = await open(stagedExecutable, "r");
      try {
        await stagedHandle.sync();
      } finally {
        await stagedHandle.close();
      }
      onProgress({
        stage: UpdateProgressStage.Installing,
        message: "Installing the verified standalone executable.",
      });
      try {
        await this.executableReplacer.replace(stagedExecutable, destination);
        replaced = true;
      } catch (cause) {
        throw new UpdateReplacementError(
          "ATOMIC_REPLACEMENT_FAILED",
          "The verified executable could not replace the current installation.",
          cause,
        );
      }
      onProgress({
        stage: UpdateProgressStage.Complete,
        message: "The verified update is ready. Restart Conduit to use it.",
      });
    } finally {
      await rm(temporaryDirectory, { recursive: true, force: true }).catch(
        () => undefined,
      );
      if (!replaced) {
        await access(destination, constants.R_OK).catch((cause) => {
          throw new UpdateReplacementError(
            "INSTALLATION_RECOVERY_FAILED",
            "The previous Conduit installation could not be verified after failure.",
            cause,
          );
        });
      }
    }
  }
}
