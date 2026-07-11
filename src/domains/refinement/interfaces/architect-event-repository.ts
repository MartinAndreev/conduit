import type { ArchitectEvent } from "../types/architect-event.js";

export interface ArchitectEventRepository {
  loadEvents(featureId: string): Promise<readonly ArchitectEvent[]>;
}
