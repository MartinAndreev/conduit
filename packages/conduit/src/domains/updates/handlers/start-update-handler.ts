import { UpdateProgressStage } from "../enums/update-progress-stage.js";
import { UpdateStatus } from "../enums/update-status.js";
import { UpdateError } from "../errors/update-errors.js";
import type {
  StartUpdateCommand,
  StartUpdateResult,
} from "../interfaces/commands/start-update.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import type { UpdateProgressEvent } from "../types/update-progress.js";
import type { UpdateStatusRepository } from "../interfaces/update-status-repository.js";
import type { Result } from "../../../system/result.js";
import type { ApplicationError } from "../../../system/bus/command-bus.js";
import { compareSemanticVersionStrings } from "../helpers/semver.js";
import { UpdateValidationError } from "../errors/update-errors.js";

export function createStartUpdateHandler(
  installer: UpdateInstaller,
  currentVersion: string,
  statusRepository?: UpdateStatusRepository,
): (
  command: StartUpdateCommand,
) => Promise<Result<StartUpdateResult, ApplicationError>> {
  return async (command) => {
    let progress: UpdateProgressEvent = {
      stage: UpdateProgressStage.Preparing,
      message: "Preparing the update.",
    };
    try {
      const precedence = compareSemanticVersionStrings(
        command.release.version,
        currentVersion,
      );
      if (precedence === undefined || precedence <= 0)
        throw new UpdateValidationError(
          "INVALID_UPDATE_PRECEDENCE",
          "The target must be a newer stable Conduit version.",
        );
      statusRepository?.set({
        schemaVersion: 1,
        status: UpdateStatus.Updating,
        currentVersion,
        targetVersion: command.release.version,
        release: command.release,
        installation: command.installation,
        progress,
      });
      await installer.install(
        {
          currentVersion,
          release: command.release,
          installation: command.installation,
        },
        (event) => {
          progress = event;
          statusRepository?.set({
            schemaVersion: 1,
            status: UpdateStatus.Updating,
            currentVersion,
            targetVersion: command.release.version,
            release: command.release,
            installation: command.installation,
            progress,
          });
        },
      );
      const status: StartUpdateResult = {
        schemaVersion: 1,
        status: UpdateStatus.Succeeded,
        currentVersion,
        targetVersion: command.release.version,
        release: command.release,
        installation: command.installation,
        progress: {
          stage: UpdateProgressStage.Complete,
          message: "The update is ready for the next launch.",
        },
      };
      statusRepository?.set(status);
      return {
        success: true,
        data: status,
      };
    } catch (cause) {
      const message =
        cause instanceof UpdateError
          ? cause.message
          : "The update could not be completed.";
      const status: StartUpdateResult = {
        schemaVersion: 1,
        status: UpdateStatus.Failed,
        currentVersion,
        targetVersion: command.release.version,
        release: command.release,
        installation: command.installation,
        progress,
        message,
        retryable: cause instanceof UpdateError ? cause.retryable : false,
      };
      statusRepository?.set(status);
      return {
        success: true,
        data: status,
      };
    }
  };
}
