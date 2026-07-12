import type { Query } from "../../../../system/bus/query-bus.js";
import type { ArchitectEvent } from "../../types/architect-event.js";

export interface GetArchitectEventsQuery extends Query {
  readonly type: "getArchitectEvents";
  readonly featureId: string;
}

export interface GetArchitectEventsReadModel {
  readonly events: readonly ArchitectEvent[];
  readonly uniqueFiles: readonly string[];
}
