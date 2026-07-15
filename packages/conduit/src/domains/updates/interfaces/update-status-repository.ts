import type { UpdateStatusReadModel } from "../types/update-status-read-model.js";

export interface UpdateStatusRepository {
  get(): UpdateStatusReadModel;
  set(status: UpdateStatusReadModel): void;
}
