import path from "node:path";
import { execFileSync } from "node:child_process";
import ora from "ora";
export interface CommandRuntimeDependencies {
  output: (message: string) => void;
  progress: <T>(
    text: string,
    work: (params?: { setText?: (text: string) => void }) => Promise<T>,
  ) => Promise<T>;
}

export function resolveProject(project?: string): string {
  return path.resolve(project ?? process.cwd());
}

export function isGitRepository(directory: string): boolean {
  try {
    execFileSync(
      "git",
      ["-C", directory, "rev-parse", "--is-inside-work-tree"],
      { stdio: "ignore" },
    );
    return true;
  } catch {
    return false;
  }
}

export async function progress<T>(
  text: string,
  work: (params?: { setText?: (text: string) => void }) => Promise<T>,
): Promise<T> {
  const spinner = ora({
    text,
    isEnabled: Boolean(process.stdout.isTTY),
  }).start();
  try {
    const result = await work({
      setText: (nextText: string) => {
        spinner.text = nextText;
      },
    });
    spinner.succeed(text);
    return result;
  } catch (error) {
    spinner.fail(text);
    throw error;
  }
}

export function defaultDependencies<T extends object>(
  dependencies: Partial<T>,
): T & CommandRuntimeDependencies {
  return {
    output: console.log,
    progress,
    ...dependencies,
  } as T & CommandRuntimeDependencies;
}
