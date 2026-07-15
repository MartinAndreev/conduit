import { UpdateStatus } from "../enums/update-status.js";
import { UpdateErrorKind } from "../enums/update-error-kind.js";
import { UpdateError } from "../errors/update-errors.js";
import type { ReleaseDiscovery } from "../interfaces/release-discovery.js";
import type {
  CheckForUpdateQuery,
  CheckForUpdateResult,
} from "../interfaces/queries/check-for-update.js";
import type { Result } from "../../../system/result.js";
import type { ApplicationError } from "../../../system/bus/query-bus.js";

export function createCheckForUpdateHandler(
  discovery: ReleaseDiscovery,
  currentVersion: string,
): (
  query: CheckForUpdateQuery,
) => Promise<Result<CheckForUpdateResult, ApplicationError>> {
  return async (_query) => {
    try {
      const release = await discovery.discover(currentVersion);
      return {
        success: true,
        data: release
          ? {
              schemaVersion: 1,
              status: UpdateStatus.Available,
              currentVersion,
              targetVersion: release.version,
              release,
            }
          : {
              schemaVersion: 1,
              status: UpdateStatus.Current,
              currentVersion,
              message: "Conduit is up to date.",
            },
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
      return {
        success: true,
        data: {
          schemaVersion: 1,
          status: UpdateStatus.Unavailable,
          currentVersion,
          message: error.message,
          retryable: error.retryable,
        },
      };
    }
  };
}
