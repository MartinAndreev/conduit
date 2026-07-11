import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { PortraitRegistry } from "../interfaces/portrait-registry.js";
import type {
  ListPortraitsQuery,
  ListPortraitsReadModel,
} from "../interfaces/queries/list-portraits.js";

export function createListPortraitsHandler(
  registry: PortraitRegistry,
): QueryHandler<ListPortraitsQuery, ListPortraitsReadModel> {
  return async () => {
    const portraits = registry.getAllPortraits();
    return { success: true, data: { portraits } };
  };
}
