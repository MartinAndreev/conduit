import { UpdateProgressStage } from "../enums/update-progress-stage.js";
import { UpdateStatus } from "../enums/update-status.js";
import { UpdateError } from "../errors/update-errors.js";
import type {
  StartUpdateCommand,
  StartUpdateResult,
} from "../interfaces/commands/start-update.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import type { UpdateProgressEvent } from "../types/update-progress.js";
import type { Result } from "../../../system/result.js";
import type { ApplicationError } from "../../../system/bus/command-bus.js";

export function createStartUpdateHandler(
  installer: UpdateInstaller,
  currentVersion: string,
): (
  command: StartUpdateCommand,
) => Promise<Result<StartUpdateResult, ApplicationError>> {
  return async (command) => {
    let progress: UpdateProgressEvent = {
      stage: UpdateProgressStage.Preparing,
      message: "Preparing the update.",
    };
    try {
      await installer.install(
        {
          currentVersion,
          release: command.release,
          installation: command.installation,
        },
        (event) => {
          progress = event;
        },
      );
      return {
        success: true,
        data: {
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
        },
      };
    } catch (cause) {
      const message =
        cause instanceof UpdateError
          ? cause.message
          : "The update could not be completed.";
      return {
        success: true,
        data: {
          schemaVersion: 1,
          status: UpdateStatus.Failed,
          currentVersion,
          targetVersion: command.release.version,
          release: command.release,
          installation: command.installation,
          progress,
          message,
          retryable: cause instanceof UpdateError ? cause.retryable : false,
        },
      };
    }
  };
}
