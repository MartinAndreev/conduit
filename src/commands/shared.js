import path from "node:path";
import { execFileSync } from "node:child_process";
import ora from "ora";

export function resolveProject(project) {
  return path.resolve(project ?? process.cwd());
}

export function isGitRepository(directory) {
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

export async function progress(text, work) {
  const spinner = ora({
    text,
    isEnabled: Boolean(process.stdout.isTTY),
  }).start();
  try {
    const result = await work({
      setText: (nextText) => {
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

export function defaultDependencies(dependencies) {
  return { output: console.log, progress, ...dependencies };
}
