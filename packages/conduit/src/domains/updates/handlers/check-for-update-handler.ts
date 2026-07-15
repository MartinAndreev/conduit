import { UpdateStatus } from "../enums/update-status.js";
import { UpdateErrorKind } from "../enums/update-error-kind.js";
import { UpdateError } from "../errors/update-errors.js";
import type { ReleaseDiscovery } from "../interfaces/release-discovery.js";
import type { InstallationDetector } from "../interfaces/installation-detector.js";
import type { UpdateStatusRepository } from "../interfaces/update-status-repository.js";
import type {
  CheckForUpdateQuery,
  CheckForUpdateResult,
} from "../interfaces/queries/check-for-update.js";
import type { Result } from "../../../system/result.js";
import type { ApplicationError } from "../../../system/bus/query-bus.js";

export function createCheckForUpdateHandler(
  discovery: ReleaseDiscovery,
  currentVersion: string,
  installationDetector?: InstallationDetector,
  statusRepository?: UpdateStatusRepository,
): (
  query: CheckForUpdateQuery,
) => Promise<Result<CheckForUpdateResult, ApplicationError>> {
  return async (_query) => {
    statusRepository?.set({
      schemaVersion: 1,
      status: UpdateStatus.Checking,
      currentVersion,
    });
    try {
      const release = await discovery.discover(currentVersion);
      const installation =
        release && installationDetector
          ? await installationDetector.detect()
          : undefined;
      const status: CheckForUpdateResult = release
        ? {
            schemaVersion: 1,
            status: UpdateStatus.Available,
            currentVersion,
            targetVersion: release.version,
            release,
            ...(installation ? { installation } : {}),
          }
        : {
            schemaVersion: 1,
            status: UpdateStatus.Current,
            currentVersion,
            message: "Conduit is up to date.",
          };
      statusRepository?.set(status);
      return {
        success: true,
        data: status,
      };
    } catch (cause) {
      const error =
        cause instanceof UpdateError
          ? cause
          : new UpdateError(
              UpdateErrorKind.Discovery,
              "DISCOVERY_FAILED",
              "The release check is unavailable.",
              true,
              { cause },
            );
      const status: CheckForUpdateResult = {
        schemaVersion: 1,
        status: UpdateStatus.Unavailable,
        currentVersion,
        message: error.message,
        retryable: error.retryable,
      };
      statusRepository?.set(status);
      return {
        success: true,
        data: status,
      };
    }
  };
}
