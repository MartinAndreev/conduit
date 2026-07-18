import type { RunDiffResult } from "../types/review.js";

export interface DiffReader {
  readDiff(worktree: string, baseline?: string): RunDiffResult;
}
