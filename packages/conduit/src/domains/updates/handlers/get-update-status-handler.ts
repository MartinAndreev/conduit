import type { GetUpdateStatusQuery } from "../interfaces/queries/get-update-status.js";
import type { UpdateStatusRepository } from "../interfaces/update-status-repository.js";
import type { ApplicationError } from "../../../system/bus/query-bus.js";
import type { Result } from "../../../system/result.js";
import type { UpdateStatusReadModel } from "../types/update-status-read-model.js";

export function createGetUpdateStatusHandler(
  repository: UpdateStatusRepository,
): (
  query: GetUpdateStatusQuery,
) => Promise<Result<UpdateStatusReadModel, ApplicationError>> {
  return async (_query) => ({ success: true, data: repository.get() });
}
