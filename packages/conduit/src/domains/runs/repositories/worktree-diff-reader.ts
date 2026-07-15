import { spawnSync } from "node:child_process";
import { lstatSync, readFileSync } from "node:fs";
import path from "node:path";
import type { DiffReader } from "../interfaces/diff-reader.js";
import type { ChangedFile, RunDiffResult } from "../types/review.js";

const CONDUIT_ARTIFACT_GLOBS = [".conduit", ".conduit/**"] as const;

function mainWorktreeFor(worktree: string): string | undefined {
  const result = spawnSync(
    "git",
    ["-C", worktree, "worktree", "list", "--porcelain"],
    {
      encoding: "utf8",
    },
  );
  const firstLine = result.stdout.split("\n", 1)[0];
  return result.status === 0 && firstLine?.startsWith("worktree ")
    ? firstLine.slice("worktree ".length)
    : undefined;
}

function addGitIgnoredPaths(
  repository: string,
  candidates: readonly string[],
  artifacts: Set<string>,
): void {
  if (candidates.length === 0) {
    return;
  }
  const result = spawnSync(
    "git",
    ["-C", repository, "check-ignore", "--no-index", "-z", "--stdin"],
    {
      encoding: "utf8",
      input: `${candidates.join("\0")}\0`,
    },
  );
  if (result.status !== 0 && result.status !== 1) {
    return;
  }
  for (const filePath of result.stdout.split("\0")) {
    if (filePath) {
      artifacts.add(filePath);
    }
  }
}

function buildArtifactMap(
  worktree: string,
  candidates: readonly string[],
): ReadonlySet<string> {
  const artifacts = new Set(
    candidates.filter((filePath) =>
      CONDUIT_ARTIFACT_GLOBS.some((pattern) =>
        path.matchesGlob(filePath, pattern),
      ),
    ),
  );
  const repositories = new Set<string>([worktree]);
  const mainWorktree = mainWorktreeFor(worktree);
  if (mainWorktree) {
    repositories.add(mainWorktree);
  }
  for (const repository of repositories) {
    addGitIgnoredPaths(repository, candidates, artifacts);
  }
  return artifacts;
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
      const untrackedFiles = untrackedResult.stdout.trim().split("\n");
      const artifactMap = buildArtifactMap(worktree, untrackedFiles);
      for (const filePath of untrackedFiles) {
        if (artifactMap.has(filePath)) {
          continue;
        }
        const absolutePath = path.join(worktree, filePath);
        if (!lstatSync(absolutePath).isFile()) {
          continue;
        }
        const content = readFileSync(absolutePath, "utf8");
        const additions = content.length ? content.split("\n").length : 0;
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
    const normalizedDiff = diff.trim() ? diff.trim() : undefined;

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
