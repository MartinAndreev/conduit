import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetRunEventsQuery,
  GetRunEventsReadModel,
} from "../interfaces/queries/get-run-events.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";

interface RunEventRepositoryWithRoleIds extends RunEventRepository {
  loadRoleIds(runId: string): Promise<readonly string[]>;
}

export function createGetRunEventsHandler(
  eventRepository: RunEventRepositoryWithRoleIds,
): QueryHandler<GetRunEventsQuery, GetRunEventsReadModel> {
  return async (query) => {
    try {
      const events = query.roleId
        ? await eventRepository.loadByRole(query.runId, query.roleId)
        : await eventRepository.loadByRun(query.runId);
      const roleIds = await eventRepository.loadRoleIds(query.runId);
      return {
        success: true,
        data: { events, roleIds },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "RUN_EVENTS_ERROR",
          message: `Failed to load run events: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
