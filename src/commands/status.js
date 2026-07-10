import { defaultDependencies, resolveProject } from "./shared.js";

export async function statusCommand(options, dependencies) {
  const { output, loadConfig, latestRuns, startDashboard } =
    defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project);
  const config = await loadConfig(projectRoot);
  const runs = await latestRuns(projectRoot, config);
  if (!runs.length) {
    output("No Conduit runs yet.");
    return [];
  }
  if (options.tui) return startDashboard({ projectRoot, config, runs });
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
