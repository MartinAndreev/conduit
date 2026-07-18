import { spawnSync } from "node:child_process";
import path from "node:path";
import type { GitWorktreeRegistration } from "../types/worktree-registration.js";

function runGit(projectRoot: string, args: readonly string[]) {
  return spawnSync("git", ["-C", projectRoot, ...args], { encoding: "utf8" });
}

export class GitWorktreeRegistryService {
  list(projectRoot: string): readonly GitWorktreeRegistration[] {
    const result = runGit(projectRoot, ["worktree", "list", "--porcelain"]);
    if (result.status !== 0)
      throw new Error("Could not inspect registered Git worktrees.");
    return result.stdout
      .trim()
      .split("\n\n")
      .filter(Boolean)
      .flatMap((block) => {
        const lines = block.split("\n");
        const workspace = lines.find((line) => line.startsWith("worktree "));
        if (!workspace) return [];
        const head = lines.find((line) => line.startsWith("HEAD "))?.slice(5);
        const branch = lines
          .find((line) => line.startsWith("branch refs/heads/"))
          ?.slice("branch refs/heads/".length);
        return [
          {
            workspacePath: path.resolve(workspace.slice("worktree ".length)),
            ...(head ? { head } : {}),
            ...(branch ? { branch } : {}),
            prunable: lines.some((line) => line.startsWith("prunable ")),
            locked: lines.some(
              (line) => line === "locked" || line.startsWith("locked "),
            ),
          },
        ];
      });
  }

  find(
    projectRoot: string,
    workspacePath: string,
  ): GitWorktreeRegistration | undefined {
    const expected = path.resolve(workspacePath);
    return this.list(projectRoot).find(
      (registration) => registration.workspacePath === expected,
    );
  }

  add(
    projectRoot: string,
    workspacePath: string,
    branch: string,
    startPoint: string,
    hooksPath: string,
  ): string | undefined {
    const result = spawnSync(
      "git",
      [
        "-c",
        `core.hooksPath=${hooksPath}`,
        "-C",
        projectRoot,
        "worktree",
        "add",
        "-b",
        branch,
        path.resolve(workspacePath),
        startPoint,
      ],
      { encoding: "utf8" },
    );
    return result.status === 0 ? undefined : result.stderr.trim();
  }

  remove(projectRoot: string, workspacePath: string): boolean {
    const registration = this.find(projectRoot, workspacePath);
    if (!registration || registration.locked) return false;
    return (
      runGit(projectRoot, [
        "worktree",
        "remove",
        "--force",
        registration.workspacePath,
      ]).status === 0
    );
  }
}

export const gitWorktreeRegistry = new GitWorktreeRegistryService();
