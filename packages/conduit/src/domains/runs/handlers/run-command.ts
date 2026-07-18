import {
  defaultDependencies,
  resolveProject,
} from "../../../system/cli/command-support.js";
import type { RunResult } from "../types/run.js";
import type { ApplicationDependencies } from "../../../system/bootstrap/types.js";
import type { CommandRuntimeDependencies } from "../../../system/cli/command-support.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunProcessRegistry } from "../repositories/run-process-registry.js";
import type { RuntimeEventRepository } from "../interfaces/runtime-event-repository.js";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import { retainClaimedRoleWorkspaces } from "../services/retain-role-workspaces-service.js";

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
  Partial<CommandRuntimeDependencies> & {
    runRecoveryRepository?: RunRecoveryRepository;
    runEventRepository?: RunEventRepository;
    runProcessRegistry?: RunProcessRegistry;
    runtimeEventRepository?: RuntimeEventRepository;
    resultRecordRepository?: ConduitResultRecordRepository;
    roleWorkspaceRepository?: import("../interfaces/role-workspace-repository.js").RoleWorkspaceRepository;
    resumeRun?: (runId: string) => Promise<import("../types/run.js").Run>;
    getResumeEligibility?: (
      runId: string,
    ) => Promise<import("../types/resume-eligibility.js").ResumeEligibility>;
    getWorkspaceContinuity?: (
      featureId: string,
      roleNames: readonly string[],
    ) => Promise<
      import("../types/workspace-continuity.js").WorkspaceContinuity
    >;
    prepareStartNew?: (
      run: import("../types/run.js").Run,
      continuity: import("../types/workspace-continuity.js").WorkspaceContinuity,
    ) => Promise<void>;
    startFeatureRun?: (
      command: import("../interfaces/commands/start-feature-run.js").StartFeatureRunCommand,
    ) => Promise<
      import("../interfaces/commands/start-feature-run.js").StartFeatureRunResult
    >;
  };

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
    getResumeEligibility,
  } = defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project as string | undefined);
  const config = await loadConfig(projectRoot);
  const roleNames = (options.roles as string)
    .split(",")
    .map((name: string) => name.trim())
    .filter(Boolean);
  if (dependencies.startFeatureRun) {
    if (options.continue && options.startNew)
      throw new Error("Choose either --continue or --start-new, not both.");
    const started = await dependencies.startFeatureRun({
      type: "startFeatureRun",
      featureId,
      roleNames,
      mode: options.continue ? "continue" : "start-new",
      confirmDiscardRetained: options.confirmDiscardRetained === true,
      waitForCompletion: true,
      dryRun: Boolean(options.dryRun),
      fetchSkills: Boolean(options.fetchSkills),
    });
    const results = [...(started.results ?? [])];
    for (const result of results) {
      output(
        `${result.role}: ${result.status}${result.files?.length ? ` · ${result.files.length} file${result.files.length === 1 ? "" : "s"} changed` : ""}`,
      );
      for (const file of result.files ?? []) output(`  ${file}`);
    }
    return results;
  }
  const continuity = await dependencies.getWorkspaceContinuity?.(
    featureId,
    roleNames,
  );
  if (options.continue && options.startNew)
    throw new Error("Choose either --continue or --start-new, not both.");
  if (options.continue) {
    if (continuity?.state !== "compatible-continue")
      throw new Error(
        continuity && "reason" in continuity
          ? continuity.reason
          : "No compatible retained run is available.",
      );
    if (!dependencies.resumeRun)
      throw new Error("Resume command is unavailable.");
    await dependencies.resumeRun(continuity.runId);
    return [];
  }
  if (continuity?.state === "lease-conflict")
    throw new Error(continuity.reason);
  if (continuity && continuity.state !== "no-retained" && !options.startNew)
    throw new Error(
      "Retained role work exists; use --continue or --start-new with --confirm-discard-retained.",
    );
  if (
    options.startNew &&
    continuity?.state !== "no-retained" &&
    options.confirmDiscardRetained !== true
  )
    throw new Error("--start-new requires --confirm-discard-retained.");
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
  if (options.startNew && continuity && continuity.state !== "no-retained")
    run.status = "failed";
  const snapshot = await dependencies.runRecoveryRepository?.saveSnapshot(run);
  let snapshotVersion = snapshot?.version;
  if (options.startNew && continuity && continuity.state !== "no-retained") {
    if (!dependencies.prepareStartNew)
      throw new Error("Start Anew preparation is unavailable.");
    try {
      await dependencies.prepareStartNew(run, continuity);
      run.status = "planned";
      const advanced = await dependencies.runRecoveryRepository?.saveSnapshot(
        run,
        snapshotVersion,
      );
      snapshotVersion = advanced?.version;
    } catch (cause) {
      run.status = "failed";
      if (dependencies.roleWorkspaceRepository)
        await retainClaimedRoleWorkspaces(
          run,
          dependencies.roleWorkspaceRepository,
        ).catch(() => undefined);
      throw cause;
    }
  }
  let snapshotWrite = Promise.resolve();
  const persistSnapshot = (): Promise<void> => {
    const repository = dependencies.runRecoveryRepository;
    if (!repository) return Promise.resolve();
    snapshotWrite = snapshotWrite.then(async () => {
      const persisted = await repository.saveSnapshot(run, snapshotVersion);
      snapshotVersion = persisted.version;
    });
    return snapshotWrite;
  };
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
      eventRepository: dependencies.runEventRepository,
      runtimeEventRepository: dependencies.runtimeEventRepository,
      resultRepository: dependencies.resultRecordRepository,
      processRegistry: dependencies.runProcessRegistry,
      onRoleWorkspaceReady: persistSnapshot,
      roleWorkspaceRepository: dependencies.roleWorkspaceRepository,
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
    await persistSnapshot();
    if (run.status === "cancelled")
      await dependencies.runRecoveryRepository?.markCancelled(run.id);
  } catch (error) {
    run.status = "failed";
    if (dependencies.roleWorkspaceRepository)
      await retainClaimedRoleWorkspaces(
        run,
        dependencies.roleWorkspaceRepository,
      ).catch(() => undefined);
    await persistSnapshot().catch(() => undefined);
    await dependencies.runRecoveryRepository?.markInterrupted(
      run.id,
      error instanceof Error ? error.message : String(error),
    );
    throw error;
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
      onResumeRun: run.status === "failed" ? dependencies.resumeRun : undefined,
      getResumeEligibility,
    });
  return results;
}
