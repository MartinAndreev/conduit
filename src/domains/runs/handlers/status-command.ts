import {
  defaultDependencies,
  resolveProject,
} from "../../../system/cli/command-support.js";
import type { Run } from "../types/run.js";
import type { ApplicationDependencies } from "../../../system/bootstrap/types.js";
import type { CommandRuntimeDependencies } from "../../../system/cli/command-support.js";

type StatusCommandDependencies = Pick<
  ApplicationDependencies,
  | "loadConfig"
  | "latestRuns"
  | "startDashboard"
  | "readRunRoleLog"
  | "readRunRolePatch"
> &
  Partial<CommandRuntimeDependencies>;

export async function statusCommand(
  options: Record<string, unknown>,
  dependencies: Partial<StatusCommandDependencies>,
): Promise<Run[]> {
  const {
    output,
    loadConfig,
    latestRuns,
    startDashboard,
    readRunRoleLog,
    readRunRolePatch,
  } = defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project as string | undefined);
  const config = await loadConfig(projectRoot);
  const runs = await latestRuns(projectRoot, config);
  if (!runs.length) {
    output("No Conduit runs yet.");
    return [];
  }
  if (options.tui) {
    await startDashboard({
      projectRoot,
      config,
      runs,
      selectedRunId: runs[0].id,
      readRoleLog: readRunRoleLog,
      readRolePatch: readRunRolePatch,
    });
    return runs;
  }
  for (const run of runs) {
    output(`${run.id}  ${run.status}`);
    for (const role of run.roles)
      output(
        `  ${role.status === "planned" ? "○" : "●"} ${role.name.padEnd(12)} ${role.runner}`,
      );
    output("");
  }
  return runs;
}
