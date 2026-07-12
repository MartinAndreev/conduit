import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunnerEvent } from "../types/runner-events.js";

interface StoredEvents {
  readonly events: readonly RunnerEvent[];
}

export class FileRunEventRepository implements RunEventRepository {
  constructor(private readonly stateDir: string) {}

  private eventsPath(runId: string): string {
    return path.join(this.stateDir, "runs", runId, "events.json");
  }

  async append(event: RunnerEvent): Promise<void> {
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
    const filePath = this.eventsPath(runId);
    await writeFile(filePath, JSON.stringify({ events: [] }, null, 2));
  }
}
