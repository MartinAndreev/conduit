import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetArchitectEventsQuery,
  GetArchitectEventsReadModel,
} from "../interfaces/queries/get-architect-events.js";
import type { ArchitectEventRepository } from "../interfaces/architect-event-repository.js";

export function createGetArchitectEventsHandler(
  repository: ArchitectEventRepository,
): QueryHandler<GetArchitectEventsQuery, GetArchitectEventsReadModel> {
  return async (query) => {
    try {
      const events = await repository.loadEvents(query.featureId);
      const uniqueFiles = [
        ...new Set(events.flatMap((e) => (e.files ? [...e.files] : []))),
      ];
      return {
        success: true,
        data: { events, uniqueFiles },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "ARCHITECT_EVENTS_ERROR",
          message: `Failed to load architect events: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
