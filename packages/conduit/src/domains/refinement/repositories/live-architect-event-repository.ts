import type { ArchitectEventRepository } from "../interfaces/architect-event-repository.js";
import type { ArchitectEvent } from "../types/architect-event.js";

export class LiveArchitectEventRepository implements ArchitectEventRepository {
  constructor(
    private readonly live: ArchitectEventRepository,
    private readonly canonical: ArchitectEventRepository,
  ) {}

  async loadEvents(featureId: string): Promise<readonly ArchitectEvent[]> {
    const liveEvents = await this.live.loadEvents(featureId);
    if (liveEvents.length > 0) {
      return liveEvents;
    }
    return this.canonical.loadEvents(featureId);
  }
}
