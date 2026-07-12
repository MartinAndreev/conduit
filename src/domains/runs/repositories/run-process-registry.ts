import type { ChildProcess } from "node:child_process";

export interface RunProcessEntry {
  readonly runId: string;
  readonly roleId: string;
  readonly process: ChildProcess;
  readonly abortController: AbortController;
}

export interface RunProcessRegistry {
  register(entry: RunProcessEntry): void;
  get(runId: string, roleId: string): RunProcessEntry | undefined;
  getByRun(runId: string): readonly RunProcessEntry[];
  cancel(runId: string): boolean;
  remove(runId: string, roleId: string): void;
}

export function createRunProcessRegistry(): RunProcessRegistry {
  const entries = new Map<string, RunProcessEntry>();

  function key(runId: string, roleId: string): string {
    return `${runId}:${roleId}`;
  }

  return {
    register(entry: RunProcessEntry): void {
      entries.set(key(entry.runId, entry.roleId), entry);
    },
    get(runId: string, roleId: string): RunProcessEntry | undefined {
      return entries.get(key(runId, roleId));
    },
    getByRun(runId: string): readonly RunProcessEntry[] {
      const result: RunProcessEntry[] = [];
      for (const entry of entries.values()) {
        if (entry.runId === runId) result.push(entry);
      }
      return result;
    },
    cancel(runId: string): boolean {
      const runEntries = [...entries.values()].filter((e) => e.runId === runId);
      let cancelled = false;
      for (const entry of runEntries) {
        entry.abortController.abort();
        if (entry.process.exitCode === null && !entry.process.killed) {
          try {
            entry.process.kill("SIGTERM");
          } catch {
            // Process may already be gone
          }
        }
        cancelled = true;
      }
      return cancelled;
    },
    remove(runId: string, roleId: string): void {
      entries.delete(key(runId, roleId));
    },
  };
}
