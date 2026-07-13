import {
  defaultDependencies,
  resolveProject,
} from "../../../system/cli/command-support.js";
import type { RunResult } from "../types/run.js";
import type { ApplicationDependencies } from "../../../system/bootstrap/types.js";
import type { CommandRuntimeDependencies } from "../../../system/cli/command-support.js";

type RunCommandDependencies = Pick<
  ApplicationDependencies,
  | "loadConfig"
  | "planRun"
  | "executeRun"
  | "builtinRoot"
  | "startWorkerRunView"
  | "startDashboard"
  | "readRunRoleLog"
  | "readRunRolePatch"
> &
  Partial<CommandRuntimeDependencies>;

export async function runCommand(
  featureId: string,
  options: Record<string, unknown>,
  dependencies: Partial<RunCommandDependencies>,
): Promise<RunResult[]> {
  const {
    output,
    progress,
    loadConfig,
    planRun,
    executeRun,
    builtinRoot,
    startWorkerRunView,
    startDashboard,
    readRunRoleLog,
    readRunRolePatch,
  } = defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project as string | undefined);
  const config = await loadConfig(projectRoot);
  const roleNames = (options.roles as string)
    .split(",")
    .map((name: string) => name.trim())
    .filter(Boolean);
  const { run, runDir } = await progress("Preparing isolated agent runs", () =>
    planRun({
      projectRoot,
      config,
      featureId,
      roleNames,
      builtinRoot,
      fetchSkills: options.fetchSkills as boolean | undefined,
    }),
  );
  const dryRun = Boolean(options.dryRun);
  const controller = new AbortController();
  const onInterrupt = () => controller.abort();
  process.once("SIGINT", onInterrupt);
  const useTui =
    !dryRun && !options.compact && process.stdin.isTTY && process.stdout.isTTY;
  let liveView:
    | Awaited<ReturnType<ApplicationDependencies["startWorkerRunView"]>>
    | undefined;
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
    } catch (error: unknown) {
      const msg = error instanceof Error ? error.message : String(error);
      output(
        `Live dashboard unavailable (${msg}); using compact progress instead.`,
      );
    }
  }
  const execute = ({
    setText = (_text: string): void => {},
  }: { setText?: (text: string) => void } = {}) =>
    executeRun({
      projectRoot,
      run,
      runDir,
      dryRun,
      signal: controller.signal,
      onProgress: (message: string) => {
        setText(`Agents · ${message}`);
        liveView?.updateStatus(message);
      },
      onChange: ({ summary, preview }: { summary: string; preview: string }) =>
        liveView?.appendEvent(`${summary}\n${preview}`),
    });
  let results: RunResult[];
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
      readRoleLog: readRunRoleLog,
      readRolePatch: readRunRolePatch,
    });
  return results;
}
