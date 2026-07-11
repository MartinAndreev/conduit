import { defaultDependencies, resolveProject } from "./shared.js";
import type { Run } from "../domains/runs/types/run.js";

export async function statusCommand(
  options: Record<string, unknown>,
  dependencies: Record<string, unknown>,
): Promise<Run[]> {
  const { output, loadConfig, latestRuns, startDashboard } =
    defaultDependencies(dependencies);
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
