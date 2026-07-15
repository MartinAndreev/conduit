export interface WorktreeLifecycleRecord {
  readonly runId: string;
  readonly status: "completed" | "failed" | "cancelled";
  readonly worktrees: readonly string[];
  readonly completedAt: string;
}
