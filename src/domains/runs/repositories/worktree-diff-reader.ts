import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
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
    const trackedDiff =
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

    const untrackedResult = spawnSync(
      "git",
      ["-C", worktree, "ls-files", "--others", "--exclude-standard"],
      { encoding: "utf8" },
    );
    const untrackedDiffs: string[] = [];
    if (untrackedResult.status === 0 && untrackedResult.stdout.trim()) {
      for (const filePath of untrackedResult.stdout.trim().split("\n")) {
        const absolutePath = path.join(worktree, filePath);
        const content = readFileSync(absolutePath, "utf8");
        const additions = content.length
          ? content.replace(/\n$/, "").split("\n").length
          : 0;
        changedFiles.push({ path: filePath, additions, deletions: 0 });
        const noIndexDiff = spawnSync(
          "git",
          ["-C", worktree, "diff", "--no-index", "--", "/dev/null", filePath],
          { encoding: "utf8" },
        );
        if (noIndexDiff.stdout.trim()) {
          untrackedDiffs.push(noIndexDiff.stdout.trim());
        }
      }
    }

    const diff = [trackedDiff, ...untrackedDiffs].filter(Boolean).join("\n");
    const trimmedDiff = diff.trim();
    const normalizedDiff = trimmedDiff || undefined;

    const totalAdditions = changedFiles.reduce(
      (sum, f) => sum + f.additions,
      0,
    );
    const totalDeletions = changedFiles.reduce(
      (sum, f) => sum + f.deletions,
      0,
    );

    return {
      diff: normalizedDiff,
      changedFiles,
      totalAdditions,
      totalDeletions,
    };
  }
}
