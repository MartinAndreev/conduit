import { spawnSync } from "node:child_process";
import type { ChangedFile, RunDiffResult } from "../types/review.js";

export interface DiffReader {
  readDiff(worktree: string): RunDiffResult;
}

export class WorktreeDiffReader implements DiffReader {
  readDiff(worktree: string): RunDiffResult {
    const diffResult = spawnSync(
      "git",
      ["-C", worktree, "diff", "--no-ext-diff", "--unified=3", "HEAD"],
      { encoding: "utf8" },
    );
    const diff =
      diffResult.status === 0 && diffResult.stdout.trim()
        ? diffResult.stdout.trim()
        : undefined;

    const numstatResult = spawnSync(
      "git",
      ["-C", worktree, "diff", "--numstat", "HEAD"],
      { encoding: "utf8" },
    );

    const changedFiles: ChangedFile[] = [];
    if (numstatResult.status === 0 && numstatResult.stdout.trim()) {
      for (const line of numstatResult.stdout.trim().split("\n")) {
        const [additions, deletions, filePath] = line.split("\t");
        if (filePath) {
          changedFiles.push({
            path: filePath,
            additions: Number(additions) || 0,
            deletions: Number(deletions) || 0,
          });
        }
      }
    }

    const totalAdditions = changedFiles.reduce(
      (sum, f) => sum + f.additions,
      0,
    );
    const totalDeletions = changedFiles.reduce(
      (sum, f) => sum + f.deletions,
      0,
    );

    return { diff, changedFiles, totalAdditions, totalDeletions };
  }
}
