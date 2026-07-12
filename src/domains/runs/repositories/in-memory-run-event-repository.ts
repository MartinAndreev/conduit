import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunnerEvent } from "../types/runner-events.js";

export class InMemoryRunEventRepository implements RunEventRepository {
  private readonly events: RunnerEvent[] = [];

  async append(event: RunnerEvent): Promise<void> {
    this.events.push(event);
  }

  async loadByRun(runId: string): Promise<readonly RunnerEvent[]> {
    return this.events.filter((event) => event.runId === runId);
  }

  async loadByRole(
    runId: string,
    roleId: string,
  ): Promise<readonly RunnerEvent[]> {
    return this.events.filter(
      (event) => event.runId === runId && event.roleId === roleId,
    );
  }

  async loadRoleIds(runId: string): Promise<readonly string[]> {
    const ids = new Set<string>();
    for (const event of this.events) {
      if (event.runId === runId) ids.add(event.roleId);
    }
    return [...ids];
  }

  async clear(runId: string): Promise<void> {
    for (let i = this.events.length - 1; i >= 0; i--) {
      if (this.events[i]!.runId === runId) this.events.splice(i, 1);
    }
  }
}
