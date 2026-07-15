import { UpdateStatus } from "../enums/update-status.js";
import type { UpdateStatusRepository } from "../interfaces/update-status-repository.js";
import type { UpdateStatusReadModel } from "../types/update-status-read-model.js";

export class InMemoryUpdateStatusRepository implements UpdateStatusRepository {
  private status: UpdateStatusReadModel;

  constructor(currentVersion: string) {
    this.status = {
      schemaVersion: 1,
      status: UpdateStatus.Idle,
      currentVersion,
    };
  }

  get(): UpdateStatusReadModel {
    return this.status;
  }

  set(status: UpdateStatusReadModel): void {
    this.status = Object.freeze(status);
  }
}
