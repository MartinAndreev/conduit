import type { UpdateStatusReadModel } from "../../types/update-status-read-model.js";

export interface CheckForUpdateQuery {
  readonly type: "checkForUpdate";
}

export type CheckForUpdateResult = UpdateStatusReadModel;
