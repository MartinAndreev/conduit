import type { RunnerEvent } from "../types/runner-events.js";

export interface RunEventRepository {
  append(event: RunnerEvent): Promise<void>;
  loadByRun(runId: string): Promise<readonly RunnerEvent[]>;
  loadByRole(runId: string, roleId: string): Promise<readonly RunnerEvent[]>;
  loadRoleIds(runId: string): Promise<readonly string[]>;
  clear(runId: string): Promise<void>;
}
