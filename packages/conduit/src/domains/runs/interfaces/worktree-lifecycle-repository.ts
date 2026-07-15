import type { WorktreeLifecycleRecord } from "../types/worktree-lifecycle.js";

export interface WorktreeLifecycleRepository {
  save(record: WorktreeLifecycleRecord): Promise<void>;
  listExpired(cutoff: Date): Promise<readonly WorktreeLifecycleRecord[]>;
  remove(runId: string): Promise<void>;
}
