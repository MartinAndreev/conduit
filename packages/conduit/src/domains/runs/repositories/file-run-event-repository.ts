import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunnerEvent } from "../types/runner-events.js";

interface StoredEvents {
  readonly events: readonly RunnerEvent[];
}

export class FileRunEventRepository implements RunEventRepository {
  private readonly pendingWrites = new Map<string, Promise<void>>();

  constructor(private readonly stateDir: string) {}

  private eventsPath(runId: string): string {
    return path.join(this.stateDir, "runs", runId, "events.json");
  }

  private enqueue(runId: string, write: () => Promise<void>): Promise<void> {
    const previous = this.pendingWrites.get(runId) ?? Promise.resolve();
    const next = previous.catch(() => {}).then(write);
    this.pendingWrites.set(runId, next);
    void next
      .finally(() => {
        if (this.pendingWrites.get(runId) === next)
          this.pendingWrites.delete(runId);
      })
      .catch(() => {});
    return next;
  }

  async append(event: RunnerEvent): Promise<void> {
    await this.enqueue(event.runId, async () => {
      const filePath = this.eventsPath(event.runId);
      await mkdir(path.dirname(filePath), { recursive: true });
      let events: RunnerEvent[] = [];
      try {
        const raw = await readFile(filePath, "utf8");
        const stored: StoredEvents = JSON.parse(raw);
        events = [...stored.events];
      } catch {
        // File doesn't exist yet
      }
      events.push(event);
      await writeFile(filePath, JSON.stringify({ events }, null, 2));
    });
  }

  async loadByRun(runId: string): Promise<readonly RunnerEvent[]> {
    try {
      const raw = await readFile(this.eventsPath(runId), "utf8");
      const stored: StoredEvents = JSON.parse(raw);
      return stored.events;
    } catch {
      return [];
    }
  }

  async loadByRole(
    runId: string,
    roleId: string,
  ): Promise<readonly RunnerEvent[]> {
    const all = await this.loadByRun(runId);
    return all.filter((e) => e.roleId === roleId);
  }

  async loadRoleIds(runId: string): Promise<readonly string[]> {
    const all = await this.loadByRun(runId);
    const ids = new Set<string>();
    for (const event of all) ids.add(event.roleId);
    return [...ids];
  }

  async clear(runId: string): Promise<void> {
    await this.enqueue(runId, async () => {
      const filePath = this.eventsPath(runId);
      await mkdir(path.dirname(filePath), { recursive: true });
      await writeFile(filePath, JSON.stringify({ events: [] }, null, 2));
    });
  }
}
