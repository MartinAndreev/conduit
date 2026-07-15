import type { InstallationStrategy } from "../../types/installation.js";
import type { StableRelease } from "../../types/release.js";
import type { UpdateStatusReadModel } from "../../types/update-status-read-model.js";

export interface StartUpdateCommand {
  readonly type: "startUpdate";
  readonly release: StableRelease;
  readonly installation: InstallationStrategy;
}

export type StartUpdateResult = UpdateStatusReadModel;
