import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { spawn, spawnSync } from "node:child_process";
import type { ChildProcess, SpawnSyncReturns } from "node:child_process";
import { resolveSkill } from "../../roles/repositories/skill-resolver.js";
import type { Config } from "../../configuration/types/config.js";
import type { Run, RunRole, RunResult } from "../types/run.js";
import type { RunnerEvent } from "../types/runner-events.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import type { RunProcessRegistry } from "./run-process-registry.js";
import { localSpecKitRoleContract } from "@domains/features/providers/local-spec-kit-role-contract.js";
import { agentResponseContractPrompt } from "../assets/agent-response-contract.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import { configureFinalOutputCapture, runnerAdapter, supportedRunners } from "@system/runners/registry.js";
import { parseAgentResponseV1 } from "../validation/agent-response-validator.js";
import { roleKindForRole, validateAgentResponseForAssignment } from "../validation/agent-semantic-validator.js";

const databaseEnvironmentKey =
  /^(?:TURSO_|LIBSQL_|DATABASE_(?:URL|TOKEN)$|CONDUIT_DB)/i;

export function agentProcessEnvironment(
  environment: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv {
  return Object.fromEntries(
    Object.entries(environment).filter(
      ([key]) => !databaseEnvironmentKey.test(key),
    ),
  );
}

export function commandForRole(
  role: {
    runner: string;
    model?: string;
    effort?: import("../../configuration/types/config.js").RoleReasoningEffort;
  },
  promptFile: string,
): [string, string[]] {
  const adapter = runnerAdapter(role.runner);
  if (!adapter)
    throw new Error(
      `Unsupported runner: ${role.runner}. Supported runners: ${supportedRunners().join(", ")}.`,
    );
  const args = [...adapter.buildArgs(promptFile, role.model)];
  if (role.effort && args.length) {
    const last = args[args.length - 1];
    args[args.length - 1] = `${last} Requested reasoning effort: ${role.effort}.`;
  }
  return [adapter.command, args];
}

function pathsOverlap(left: string[] = [], right: string[] = []): boolean {
  return left.some((a) =>
    right.some(
      (b) => a === b || a.startsWith(`${b}/`) || b.startsWith(`${a}/`),
    ),
  );
}

function assertDistinctOwnership(roles: RunRole[]): void {
  for (let index = 0; index < roles.length; index += 1) {
    for (let other = index + 1; other < roles.length; other += 1) {
      if (roles[index].readOnly || roles[other].readOnly) continue;
      if (pathsOverlap(roles[index].owns, roles[other].owns)) {
        throw new Error(
          `Roles ${roles[index].name} and ${roles[other].name} have overlapping owned paths.`,
        );
      }
    }
  }
}

export async function planRun({
  projectRoot,
  config,
  featureId,
  roleNames,
  builtinRoot,
  fetchSkills = false,
}: {
  projectRoot: string;
  config: Config;
  featureId: string;
  roleNames: string[];
  builtinRoot: string;
  fetchSkills?: boolean;
}): Promise<{ run: Run; runDir: string }> {
  const runId = `${featureId}-${Date.now()}`;
  const runDir = path.join(projectRoot, config.stateDir, "runs", runId);
  await mkdir(runDir, { recursive: true });
  const roles: RunRole[] = [];
  for (const name of roleNames) {
    const role = config.roles[name];
    if (!role) throw new Error(`Unknown role: ${name}`);
    const skill = await resolveSkill({
      projectRoot,
      roleName: name,
      role,
      builtinRoot,
      allowNetwork: fetchSkills,
    }).catch(() => ({
      source: role.skill.source,
      content: "No project role guidance was loaded.",
      verified: false,
    }));
    const promptFile = path.join(runDir, `${name}.md`);
    const roleDependencies = role.dependsOn ?? [];
    const prompt = redactSecrets(
      `${localSpecKitRoleContract(name, role.effort)}\n\n# Project role guidance (advisory)\n\n${skill.content}\n\n# Assignment (authoritative)\n\nFeature: ${featureId}\nRead the approved files in specs/${featureId}-*/ before changing code.\nOwned paths: ${(role.owns ?? ["none defined"]).join(", ")}\nRun dependencies: ${roleDependencies.length ? roleDependencies.join(", ") : "none"}\nDo not modify contracts or paths outside your ownership.\nReport tests run and unresolved integration risks.\n\n${agentResponseContractPrompt()}\n\nThe system role contract and assignment take precedence over project role guidance.`,
    );
    await writeFile(promptFile, prompt);
    const [command, args] = commandForRole(role, promptFile);
    roles.push({
      name,
      runner: role.runner,
      model: role.model,
      effort: role.effort,
      readOnly: Boolean(role.readOnly),
      owns: role.owns ?? [],
      dependsOn: roleDependencies,
      promptFile,
      prompt,
      command,
      args,
      skillSource: skill.source,
      status: "planned",
      finalOutputFile: path.join(runDir, `${name}-agent-response.json`),
    });
  }
  assertDistinctOwnership(roles);
  const run: Run = {
    id: runId,
    featureId,
    status: "planned",
    createdAt: new Date().toISOString(),
    roles,
  };
  return { run, runDir };
}

function worktreePath(projectRoot: string, run: Run, role: RunRole): string {
  return path.join(
    path.dirname(projectRoot),
    ".conduit-worktrees",
    path.basename(projectRoot),
    run.id,
    role.name,
  );
}

function addWorktree(projectRoot: string, run: Run, role: RunRole): string {
  // A newly initialized repository can have an unborn HEAD. Git cannot make
  // a worktree from it, but roles can still work safely in the project root
  // when their configured ownership does not overlap.
  const head = spawnSync(
    "git",
    ["-C", projectRoot, "rev-parse", "--verify", "HEAD"],
    { encoding: "utf8" },
  );
  if (head.status !== 0) return projectRoot;
  const target = worktreePath(projectRoot, run, role);
  const branch = `conduit/${run.id}/${role.name}`;
  const result: SpawnSyncReturns<string> = spawnSync(
    "git",
    ["-C", projectRoot, "worktree", "add", "-b", branch, target, "HEAD"],
    { encoding: "utf8" },
  );
  if (result.status !== 0)
    throw new Error(
      `Could not create worktree for ${role.name}: ${result.stderr.trim()}`,
    );
  return target;
}

async function writeWorktreePrompt(
  cwd: string,
  run: Run,
  role: RunRole,
): Promise<void> {
  const assignmentDir = path.join(cwd, ".conduit", "assignments");
  const promptFile = path.join(
    assignmentDir,
    `${run.id}-${role.name}.md`,
  );
  await mkdir(path.dirname(promptFile), { recursive: true });
  await writeFile(promptFile, role.prompt);
  const [command, args] = commandForRole(role, promptFile);
  role.command = command;
  role.args = args;
  role.worktreePromptFile = promptFile;
  role.finalOutputFile = path.join(assignmentDir, `${run.id}-${role.name}-agent-response.json`);
}

function terminate(child: ChildProcess): void {
  if (child.exitCode !== null || child.killed) return;
  try {
    if (process.platform !== "win32" && child.pid)
      process.kill(-child.pid, "SIGTERM");
    else child.kill("SIGTERM");
  } catch {
    // The child may already have exited before its process group is signalled.
  }
  setTimeout(() => {
    if (child.exitCode === null && !child.killed) {
      try {
        if (process.platform !== "win32" && child.pid)
          process.kill(-child.pid, "SIGKILL");
        else child.kill("SIGKILL");
      } catch {
        // Best effort: a completed child no longer needs to be killed.
      }
    }
  }, 3000).unref();
}

function changedFiles(cwd: string): string[] {
  const result: SpawnSyncReturns<string> = spawnSync(
    "git",
    ["-C", cwd, "diff", "--name-only"],
    {
      encoding: "utf8",
    },
  );
  return result.status === 0
    ? result.stdout.trim().split("\n").filter(Boolean)
    : [];
}

interface ChangedFileStat {
  file: string;
  added: number;
  removed: number;
}

function changedSummary(cwd: string): {
  files: ChangedFileStat[];
  added: number;
  removed: number;
  preview: string;
} {
  const result: SpawnSyncReturns<string> = spawnSync(
    "git",
    ["-C", cwd, "diff", "--numstat"],
    {
      encoding: "utf8",
    },
  );
  const stats = result.status === 0 ? result.stdout.trim() : "";
  const files: ChangedFileStat[] = stats
    ? stats.split("\n").map((line) => {
        const [added, removed, file] = line.split("\t");
        return {
          file,
          added: Number(added) || 0,
          removed: Number(removed) || 0,
        };
      })
    : [];
  const added = files.reduce((total, file) => total + file.added, 0);
  const removed = files.reduce((total, file) => total + file.removed, 0);
  const preview = files
    .slice(0, 2)
    .map(({ file, added: plus, removed: minus }) => {
      const diffResult: SpawnSyncReturns<string> = spawnSync(
        "git",
        ["-C", cwd, "diff", "--unified=1", "--", file],
        { encoding: "utf8" },
      );
      const diff = diffResult.status === 0 ? diffResult.stdout : "";
      const lines = diff
        .split("\n")
        .filter(
          (line) =>
            !line.startsWith("diff --git") &&
            !line.startsWith("index ") &&
            !line.startsWith("--- ") &&
            !line.startsWith("+++ "),
        )
        .slice(0, 8);
      return `\u2514 ${file} (+${plus} -${minus})\n${lines.map((line) => `  ${line}`).join("\n")}`;
    })
    .join("\n\n");
  return { files, added, removed, preview };
}

function runProcess(
  role: RunRole,
  runId: string,
  cwd: string,
  logFile: string,
  onProgress: (message: string) => void,
  onChange: (event: { summary: string; preview: string }) => void,
  eventRepository?: RunEventRepository,
  processRegistry?: RunProcessRegistry,
  signal?: AbortSignal,
): Promise<RunResult> {
  const emitEvent = async (event: RunnerEvent) => {
    if (eventRepository) await eventRepository.append(event);
  };

  return new Promise((resolve) => {
    const args = configureFinalOutputCapture(
      role.runner,
      role.args,
      role.finalOutputFile,
    );
    const child = spawn(role.command, args, {
      cwd,
      stdio: ["ignore", "pipe", "pipe"],
      detached: process.platform !== "win32",
      env: agentProcessEnvironment(),
    });
    let output = "";
    let stdout = "";
    const adapter = runnerAdapter(role.runner);
    const stdoutParser = adapter?.createOutputParser?.(runId, role.name);
    const stderrParser = adapter?.createOutputParser?.(runId, role.name);
    let cancelled = false;
    let previousFiles = "";
    const abortController = new AbortController();

    // Register process for cancellation
    if (processRegistry) {
      processRegistry.register({
        runId,
        roleId: role.name,
        process: child,
        abortController,
      });
    }

    // Emit starting lifecycle event
    void emitEvent({
      type: "lifecycle",
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: "starting",
        message: `${role.name}: agent starting`,
      },
    });

    const cancel = () => {
      cancelled = true;
      onProgress(`${role.name}: cancelling`);
      terminate(child);
    };
    abortController.signal.addEventListener("abort", cancel, { once: true });
    if (signal?.aborted) cancel();
    signal?.addEventListener("abort", cancel, { once: true });
    onProgress(`${role.name}: agent started`);

    // Emit running lifecycle event
    void emitEvent({
      type: "lifecycle",
      runId,
      roleId: role.name,
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: "running",
        message: `${role.name}: agent started`,
      },
    });

    const changeWatcher = setInterval(() => {
      const change = changedSummary(cwd);
      const fingerprint = change.files
        .map((file) => `${file.file}:${file.added}:${file.removed}`)
        .join("\n");
      if (fingerprint && fingerprint !== previousFiles) {
        previousFiles = fingerprint;
        const summary = `${role.name}: edited ${change.files.length} file${change.files.length === 1 ? "" : "s"} (+${change.added} -${change.removed})`;
        onProgress(summary);
        onChange({ summary, preview: change.preview });
        // Emit file-change events for each changed file
        for (const file of change.files) {
          void emitEvent({
            type: "file-change",
            runId,
            roleId: role.name,
            timestamp: new Date().toISOString(),
            payload: {
              kind: "file-change",
              path: file.file,
              additions: file.added,
              deletions: file.removed,
            },
          });
        }
      }
    }, 800);
    changeWatcher.unref();
    child.stdout?.on("data", (chunk: Buffer | string) => {
      const sanitizedChunk = redactSecrets(String(chunk));
      const transcript = sanitizedChunk.trim();
      output += sanitizedChunk;
      stdout += sanitizedChunk;
      onProgress(
        transcript
          ? `${role.name}: ${transcript.replace(/\s+/g, " ").slice(0, 100)}`
          : `${role.name}: working`,
      );
      const parsedEvents = stdoutParser?.push(sanitizedChunk) ?? [];
      if (parsedEvents.length) for (const event of parsedEvents) void emitEvent(event);
      else
        void emitEvent({
          type: "tool-output",
          runId,
          roleId: role.name,
          timestamp: new Date().toISOString(),
          payload: {
            kind: "tool-output",
            tool: "runner stdout",
            output: transcript.slice(0, 4_000),
            truncated: transcript.length > 4_000,
          },
        });
    });
    child.stderr?.on("data", (chunk: Buffer | string) => {
      const sanitizedChunk = redactSecrets(String(chunk));
      const transcript = sanitizedChunk.trim();
      output += sanitizedChunk;
      onProgress(
        transcript
          ? `${role.name}: ${transcript.replace(/\s+/g, " ").slice(0, 100)}`
          : `${role.name}: working`,
      );
      const parsedEvents = stderrParser?.push(sanitizedChunk) ?? [];
      if (parsedEvents.length) for (const event of parsedEvents) void emitEvent(event);
      else
        void emitEvent({
          type: "tool-output",
          runId,
          roleId: role.name,
          timestamp: new Date().toISOString(),
          payload: {
            kind: "tool-output",
            tool: "runner stderr",
            output: transcript.slice(0, 4_000),
            truncated: transcript.length > 4_000,
          },
        });
    });
    child.on("error", (error: Error) => {
      void emitEvent({
        type: "error",
        runId,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "error",
          code: "PROCESS_ERROR",
          message: redactSecrets(error.message),
          recoverable: false,
        },
      });
      if (processRegistry) processRegistry.remove(runId, role.name);
      resolve({
        role: role.name,
        status: "failed",
        error: redactSecrets(error.message),
        output,
        stdout,
      });
    });
    child.on("close", async (code: number | null) => {
      await writeFile(logFile, output);
      clearInterval(changeWatcher);
      signal?.removeEventListener("abort", cancel);
      abortController.signal.removeEventListener("abort", cancel);
      if (processRegistry) processRegistry.remove(runId, role.name);
      const finalChange = changedSummary(cwd);
      const finalFingerprint = finalChange.files
        .map((file) => `${file.file}:${file.added}:${file.removed}`)
        .join("\n");
      if (finalFingerprint && finalFingerprint !== previousFiles) {
        const summary = `${role.name}: edited ${finalChange.files.length} file${finalChange.files.length === 1 ? "" : "s"} (+${finalChange.added} -${finalChange.removed})`;
        onChange({ summary, preview: finalChange.preview });
        for (const file of finalChange.files) {
          void emitEvent({
            type: "file-change",
            runId,
            roleId: role.name,
            timestamp: new Date().toISOString(),
            payload: {
              kind: "file-change",
              path: file.file,
              additions: file.added,
              deletions: file.removed,
            },
          });
        }
      }
      const files = changedFiles(cwd);
      const finalParserEvents = [
        ...(stdoutParser?.flush() ?? []),
        ...(stderrParser?.flush() ?? []),
      ];
      for (const event of finalParserEvents) void emitEvent(event);
      const capturedFinal = role.finalOutputFile
        ? await readFile(role.finalOutputFile, "utf8").catch(() => "")
        : "";
      const finalResponseRaw = (capturedFinal.trim() || stdoutParser?.finalResponse || stderrParser?.finalResponse || stdout).trim();
      const structural = finalResponseRaw
        ? parseAgentResponseV1(finalResponseRaw)
        : { valid: false, issues: [{ path: "$", message: "missing AgentResponseV1 final response" }] };
      const semantic = structural.valid && structural.value
        ? validateAgentResponseForAssignment(structural.value, {
            roleKind: roleKindForRole(role.name),
            ownedPaths: role.owns,
          })
        : structural;
      const protocolCompleted = Boolean(code === 0 && structural.valid && semantic.valid && structural.value?.status === "completed");
      const status: RunResult["status"] = cancelled ? "cancelled" : protocolCompleted ? "completed" : "failed";
      if (!structural.valid || !semantic.valid || structural.value?.status !== "completed") {
        const failures = [...structural.issues, ...semantic.issues].map((item) => `${item.path}: ${item.message}`).join("; ") || `AgentResponseV1 status ${structural.value?.status ?? "missing"}`;
        void emitEvent({
          type: "error",
          runId,
          roleId: role.name,
          timestamp: new Date().toISOString(),
          payload: {
            kind: "error",
            code: "AGENT_PROTOCOL_INVALID",
            message: `Run ${runId} role ${role.name} runner ${role.runner} did not return a semantically complete AgentResponseV1: ${failures}. Remediation: return exactly one valid AgentResponseV1 JSON object. Sanitized raw response is in ${logFile}.`,
            recoverable: true,
          },
        });
      }
      if (structural.valid) {
        void emitEvent({
          type: "activity",
          runId,
          roleId: role.name,
          timestamp: new Date().toISOString(),
          payload: { kind: "activity", message: `${role.name}: final AgentResponseV1 received` },
        });
      }
      onProgress(
        `${role.name}: ${status}${files.length ? ` · ${files.length} file${files.length === 1 ? "" : "s"} changed` : ""}`,
      );

      // Emit completion lifecycle event
      const lifecycleState =
        status === "completed"
          ? "completed"
          : status === "cancelled"
            ? "cancelled"
            : "failed";
      void emitEvent({
        type: "lifecycle",
        runId,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "lifecycle",
          state: lifecycleState,
          message: `${role.name}: ${status}`,
        },
      });

      // Emit result event
      void emitEvent({
        type: "result",
        runId,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "result",
          exitCode: code ?? -1,
          files,
          summary: `${role.name}: ${status}`,
        },
      });

      resolve({
        role: role.name,
        status,
        exitCode: code ?? undefined,
        output,
        stdout,
        files,
      });
    });
  });
}

function roleExecutionGroups(roles: RunRole[]): RunRole[][] {
  const selected = new Map(roles.map((role) => [role.name, role]));
  const pending = new Set(roles.map((role) => role.name));
  const groups: RunRole[][] = [];
  while (pending.size) {
    const ready = [...pending]
      .map((name) => selected.get(name)!)
      .filter((role) =>
        role.dependsOn.every(
          (dependency) => !selected.has(dependency) || !pending.has(dependency),
        ),
      );
    if (!ready.length) {
      throw new Error(
        `Role dependency cycle detected among: ${[...pending].sort().join(", ")}.`,
      );
    }
    groups.push(ready);
    for (const role of ready) pending.delete(role.name);
  }
  return groups;
}

export async function executeRun({
  projectRoot,
  run,
  runDir,
  dryRun = true,
  onProgress = () => {},
  onChange = () => {},
  signal,
  eventRepository,
  processRegistry,
}: {
  projectRoot: string;
  run: Run;
  runDir: string;
  dryRun?: boolean;
  onProgress?: (message: string) => void;
  onChange?: (event: { summary: string; preview: string }) => void;
  signal?: AbortSignal;
  eventRepository?: RunEventRepository;
  processRegistry?: RunProcessRegistry;
}): Promise<RunResult[]> {
  // Emit system-level starting event
  if (eventRepository) {
    await eventRepository.append({
      type: "lifecycle",
      runId: run.id,
      roleId: "system",
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state: "starting",
        message: "Run starting",
      },
    });
  }

  if (dryRun)
    return run.roles.map((role) => ({
      role: role.name,
      status: "dry-run" as const,
      command: [role.command, ...role.args],
    }));
  const launchRole = async (role: RunRole): Promise<RunResult> => {
    onProgress(
      `${role.name}: preparing ${role.readOnly ? "project workspace" : "isolated worktree"}`,
    );
    const cwd = role.readOnly
      ? projectRoot
      : addWorktree(projectRoot, run, role);
    role.worktree = role.readOnly || cwd === projectRoot ? undefined : cwd;
    if (!role.readOnly) await writeWorktreePrompt(cwd, run, role);
    return runProcess(
      role,
      run.id,
      cwd,
      path.join(runDir, `${role.name}.log`),
      onProgress,
      onChange,
      eventRepository,
      processRegistry,
      signal,
    );
  };
  const launchRoleSafely = async (role: RunRole): Promise<RunResult> => {
    try {
      return await launchRole(role);
    } catch (cause) {
      const message = redactSecrets(
        cause instanceof Error ? cause.message : String(cause),
      );
      await eventRepository?.append({
        type: "error",
        runId: run.id,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "error",
          code: "ROLE_LAUNCH_FAILED",
          message,
          recoverable: false,
        },
      });
      await eventRepository?.append({
        type: "lifecycle",
        runId: run.id,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "lifecycle",
          state: "failed",
          message: `${role.name}: failed to start`,
        },
      });
      return { role: role.name, status: "failed", error: message };
    }
  };
  const results: RunResult[] = [];
  const terminalByRole = new Map<string, RunResult>();
  for (const group of roleExecutionGroups(run.roles)) {
    const blocked = group.filter((role) =>
      role.dependsOn.some((dependency) =>
        terminalByRole.has(dependency)
          ? terminalByRole.get(dependency)?.status !== "completed"
          : false,
      ),
    );
    for (const role of blocked) {
      const failedDependencies = role.dependsOn.filter(
        (dependency) =>
          terminalByRole.has(dependency) &&
          terminalByRole.get(dependency)?.status !== "completed",
      );
      const result: RunResult = {
        role: role.name,
        status: "failed",
        error: `Skipped because dependencies failed: ${failedDependencies.join(", ")}`,
      };
      results.push(result);
      terminalByRole.set(role.name, result);
      await eventRepository?.append({
        type: "lifecycle",
        runId: run.id,
        roleId: role.name,
        timestamp: new Date().toISOString(),
        payload: {
          kind: "lifecycle",
          state: "failed",
          message: `${role.name}: skipped because dependencies failed`,
        },
      });
    }
    const runnable = group.filter((role) => !terminalByRole.has(role.name));
    const groupResults = await Promise.all(runnable.map(launchRoleSafely));
    for (const result of groupResults) {
      results.push(result);
      terminalByRole.set(result.role, result);
    }
  }
  const reviewerSelected = run.roles.some((role) => role.name === "reviewer");
  run.status = results.some((result) => result.status === "cancelled")
    ? "cancelled"
    : results.every((result) => result.status === "completed")
      ? "completed"
      : "failed";
  for (const role of run.roles)
    role.status =
      (results.find((result) => result.role === role.name)
        ?.status as RunRole["status"]) ?? "failed";
  const successfulRun = run.status === "completed";
  const completionMessage = successfulRun
    ? reviewerSelected
      ? "Flow finished: reviewer completed with no required fixes."
      : "Flow finished: all selected agents completed."
    : `Run ${run.status}`;

  // Emit system-level completion event
  if (eventRepository) {
    await eventRepository.append({
      type: "lifecycle",
      runId: run.id,
      roleId: "system",
      timestamp: new Date().toISOString(),
      payload: {
        kind: "lifecycle",
        state:
          run.status === "completed"
            ? "completed"
            : run.status === "cancelled"
              ? "cancelled"
              : "failed",
        message: completionMessage,
      },
    });
    if (successfulRun) {
      await eventRepository.append({
        type: "activity",
        runId: run.id,
        roleId: "system",
        timestamp: new Date().toISOString(),
        payload: {
          kind: "activity",
          message: completionMessage,
        },
      });
    }
  }

  return results;
}
