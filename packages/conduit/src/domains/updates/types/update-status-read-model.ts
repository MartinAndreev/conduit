import { UpdateStatus } from "../enums/update-status.js";
import type { InstallationStrategy } from "./installation.js";
import type { StableRelease } from "./release.js";
import type { UpdateProgressEvent } from "./update-progress.js";

interface UpdateStatusBase {
  readonly schemaVersion: 1;
  readonly currentVersion: string;
}

export interface IdleUpdateStatus extends UpdateStatusBase {
  readonly status: UpdateStatus.Idle;
}

export interface CheckingUpdateStatus extends UpdateStatusBase {
  readonly status: UpdateStatus.Checking;
}

export interface CurrentUpdateStatus extends UpdateStatusBase {
  readonly status: UpdateStatus.Current;
  readonly message: string;
}

export interface AvailableUpdateStatus extends UpdateStatusBase {
  readonly status: UpdateStatus.Available;
  readonly targetVersion: string;
  readonly release: StableRelease;
  readonly installation?: InstallationStrategy;
}

export interface UnavailableUpdateStatus extends UpdateStatusBase {
  readonly status: UpdateStatus.Unavailable;
  readonly message: string;
  readonly retryable: boolean;
}

interface ActiveUpdateStatus extends UpdateStatusBase {
  readonly targetVersion: string;
  readonly release: StableRelease;
  readonly installation: InstallationStrategy;
  readonly progress: UpdateProgressEvent;
}

export interface UpdatingUpdateStatus extends ActiveUpdateStatus {
  readonly status: UpdateStatus.Updating;
}

export interface SucceededUpdateStatus extends ActiveUpdateStatus {
  readonly status: UpdateStatus.Succeeded;
}

export interface FailedUpdateStatus extends ActiveUpdateStatus {
  readonly status: UpdateStatus.Failed;
  readonly message: string;
  readonly retryable: boolean;
}

export type UpdateStatusReadModel =
  | IdleUpdateStatus
  | CheckingUpdateStatus
  | CurrentUpdateStatus
  | AvailableUpdateStatus
  | UnavailableUpdateStatus
  | UpdatingUpdateStatus
  | SucceededUpdateStatus
  | FailedUpdateStatus;
