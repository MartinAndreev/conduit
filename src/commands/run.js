import { defaultDependencies, resolveProject } from "./shared.js";

export async function runCommand(featureId, options, dependencies) {
  const {
    output,
    progress,
    loadConfig,
    planRun,
    executeRun,
    builtinRoot,
    startWorkerRunView,
    startDashboard,
  } = defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project);
  const config = await loadConfig(projectRoot);
  const roleNames = options.roles
    .split(",")
    .map((name) => name.trim())
    .filter(Boolean);
  const { run, runDir } = await progress("Preparing isolated agent runs", () =>
    planRun({
      projectRoot,
      config,
      featureId,
      roleNames,
      builtinRoot,
      fetchSkills: options.fetchSkills,
    }),
  );
  const dryRun = Boolean(options.dryRun);
  const controller = new AbortController();
  const onInterrupt = () => controller.abort();
  process.once("SIGINT", onInterrupt);
  const useTui =
    !dryRun && !options.compact && process.stdin.isTTY && process.stdout.isTTY;
  let liveView;
  let showDashboard = false;
  if (useTui) {
    try {
      liveView = await startWorkerRunView({
        featureId,
        roles: roleNames,
        onCancel: () => controller.abort(),
        onUserClose: () => {
          showDashboard = false;
          liveView = undefined;
        },
      });
      showDashboard = true;
    } catch (error) {
      output(
        `Live dashboard unavailable (${error.message}); using compact progress instead.`,
      );
    }
  }
  const execute = ({ setText = () => {} } = {}) =>
    executeRun({
      projectRoot,
      run,
      runDir,
      dryRun,
      signal: controller.signal,
      onProgress: (message) => {
        setText(`Agents · ${message}`);
        liveView?.updateStatus(message);
      },
      onChange: ({ summary, preview }) =>
        liveView?.appendEvent(`${summary}\n${preview}`),
    });
  let results;
  try {
    results = liveView
      ? await execute()
      : await progress(
          dryRun ? "Rendering dry-run plan" : "Launching agent runs",
          execute,
        );
  } finally {
    liveView?.close();
  }
  process.removeListener("SIGINT", onInterrupt);
  if (controller.signal.aborted) process.exitCode = 130;
  for (const result of results) {
    output(
      `${result.role}: ${result.status}${result.files?.length ? ` · ${result.files.length} file${result.files.length === 1 ? "" : "s"} changed` : ""}`,
    );
    for (const file of result.files ?? []) output(`  ${file}`);
    if (result.command) output(`  ${result.command.join(" ")}`);
  }
  if (showDashboard)
    await startDashboard({
      projectRoot,
      config,
      runs: [run],
      selectedRunId: run.id,
    });
  return results;
}
