import type { UpdateStatusReadModel } from "../../types/update-status-read-model.js";

export interface GetUpdateStatusQuery {
  readonly type: "getUpdateStatus";
}

export type GetUpdateStatusResult = UpdateStatusReadModel;
