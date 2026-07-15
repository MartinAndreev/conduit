import { spawnSync } from "node:child_process";
import { lstatSync, readFileSync } from "node:fs";
import path from "node:path";
import type { DiffReader } from "../interfaces/diff-reader.js";
import type { ChangedFile, RunDiffResult } from "../types/review.js";
import {
  isUntrackedArtifactPath,
  untrackedArtifactGitExcludes,
} from "../helpers/dependency-tree-paths.js";

const CONDUIT_ARTIFACT_GLOBS = [".conduit", ".conduit/**"] as const;
const MAX_CHANGED_FILES = 250;
const MAX_FILE_DIFF_BYTES = 256 * 1024;
const MAX_TOTAL_DIFF_BYTES = 512 * 1024;

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
    candidates.filter(
      (filePath) =>
        isUntrackedArtifactPath(filePath) ||
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
    const numstatResult = spawnSync(
      "git",
      ["-C", worktree, "diff", "--numstat", "HEAD"],
      { encoding: "utf8" },
    );

    const changedFiles: ChangedFile[] = [];
    const diffFragments: string[] = [];
    let retainedDiffBytes = 0;
    const retainDiff = (candidate: string): void => {
      const normalized = candidate.trim();
      const bytes = Buffer.byteLength(normalized);
      if (
        !normalized ||
        bytes > MAX_FILE_DIFF_BYTES ||
        retainedDiffBytes + bytes > MAX_TOTAL_DIFF_BYTES
      ) {
        return;
      }
      diffFragments.push(normalized);
      retainedDiffBytes += bytes;
    };
    if (numstatResult.status === 0 && numstatResult.stdout.trim()) {
      for (const line of numstatResult.stdout
        .trim()
        .split("\n")
        .slice(0, MAX_CHANGED_FILES)) {
        const [additions, deletions, filePath] = line.split("\t");
        if (filePath) {
          changedFiles.push({
            path: filePath,
            additions: Number(additions) || 0,
            deletions: Number(deletions) || 0,
          });
          const diffResult = spawnSync(
            "git",
            [
              "-C",
              worktree,
              "diff",
              "--no-ext-diff",
              "--unified=3",
              "HEAD",
              "--",
              filePath,
            ],
            { encoding: "utf8", maxBuffer: MAX_FILE_DIFF_BYTES + 1 },
          );
          if (diffResult.status === 0) {
            retainDiff(diffResult.stdout);
          }
        }
      }
    }

    const untrackedResult = spawnSync(
      "git",
      [
        "-C",
        worktree,
        "ls-files",
        "--others",
        "--exclude-standard",
        ...untrackedArtifactGitExcludes(),
      ],
      { encoding: "utf8" },
    );
    if (untrackedResult.status === 0 && untrackedResult.stdout.trim()) {
      const untrackedFiles = untrackedResult.stdout.trim().split("\n");
      const artifactMap = buildArtifactMap(worktree, untrackedFiles);
      for (const filePath of untrackedFiles) {
        if (changedFiles.length >= MAX_CHANGED_FILES) {
          break;
        }
        if (artifactMap.has(filePath)) {
          continue;
        }
        const absolutePath = path.join(worktree, filePath);
        const stat = lstatSync(absolutePath);
        if (!stat.isFile()) {
          continue;
        }
        const content =
          stat.size <= MAX_FILE_DIFF_BYTES
            ? readFileSync(absolutePath, "utf8")
            : "";
        const additions = content.length ? content.split("\n").length : 0;
        changedFiles.push({ path: filePath, additions, deletions: 0 });
        if (stat.size > MAX_FILE_DIFF_BYTES) {
          continue;
        }
        const noIndexDiff = spawnSync(
          "git",
          ["-C", worktree, "diff", "--no-index", "--", "/dev/null", filePath],
          { encoding: "utf8", maxBuffer: MAX_FILE_DIFF_BYTES + 1 },
        );
        retainDiff(noIndexDiff.stdout);
      }
    }

    const diff = diffFragments.join("\n");
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
