import path from "node:path";
import {
  defaultDependencies,
  isGitRepository,
} from "../../../system/cli/command-support.js";
import type { CommandRuntimeDependencies } from "../../../system/cli/command-support.js";
import type { ApplicationDependencies } from "../../../system/bootstrap/types.js";

type InitCommandDependencies = Pick<
  ApplicationDependencies,
  "initializeProject" | "templatesRoot" | "roleTemplates"
> &
  Partial<CommandRuntimeDependencies>;

export async function initCommand(
  target: string,
  options: Record<string, unknown>,
  dependencies: Partial<InitCommandDependencies>,
): Promise<void> {
  const { output, progress, initializeProject, templatesRoot, roleTemplates } =
    defaultDependencies(dependencies);
  const projectRoot = path.resolve(target);
  if (!isGitRepository(projectRoot))
    throw new Error(
      `${projectRoot} is not a Git repository. Initialize Git before Conduit.`,
    );
  if (options.dryRun) {
    output(`Would initialize Conduit in ${projectRoot}`);
    return;
  }
  await progress("Initializing Conduit", () =>
    initializeProject(projectRoot, templatesRoot, roleTemplates),
  );
  output(`Conduit is ready in ${projectRoot}`);
}
