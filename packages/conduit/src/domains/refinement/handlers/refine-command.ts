import { appendFile, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { spawn } from "node:child_process";
import type { ChildProcess } from "node:child_process";
import {
  defaultDependencies,
  resolveProject,
} from "../../../system/cli/command-support.js";
import { defaultConfig } from "../../configuration/repositories/project-config.js";
import type { RefinementResult } from "../types/refinement.js";
import type { Run } from "../../runs/types/run.js";
import type { ApplicationDependencies } from "../../../system/bootstrap/types.js";
import { textarea } from "../../../tui/textarea.js";
import type { CommandRuntimeDependencies } from "../../../system/cli/command-support.js";
import { formatRefinementBrief } from "@helpers/formatting/refinement-brief.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import type { RunRecoveryRepository } from "../../runs/interfaces/run-recovery-repository.js";
import { agentProcessEnvironment } from "../../runs/repositories/run-orchestrator.js";
import { createAgentAssignmentV1 } from "../../runs/factories/agent-assignment-factory.js";
import { validateAgentAssignmentV1 } from "../../runs/validation/agent-assignment-validator.js";
import { parseAgentResponseV1 } from "../../runs/validation/agent-response-validator.js";
import { validateAgentResponseForAssignment } from "../../runs/validation/agent-semantic-validator.js";
import { renderClarificationQuestions } from "../../runs/validation/clarification-renderer.js";
import { agentResponseContractPrompt } from "../../runs/assets/agent-response-contract.js";
import {
  captureFinalResponse,
  configureFinalOutputCapture,
  runnerAdapter,
} from "@system/runners/registry.js";
import { extractArchitectEvents } from "../helpers/architect-event-parser.js";

const activeArchitectProcesses = new Map<string, ChildProcess>();

export function cancelArchitectForFeature(featureId: string): boolean {
  const entry = [...activeArchitectProcesses.entries()].find(([logFile]) =>
    logFile.includes(`refine-${featureId}-`),
  );
  if (!entry) return false;
  entry[1].kill("SIGTERM");
  return true;
}

type RefinementCommandDependencies = Pick<
  ApplicationDependencies,
  | "loadConfig"
  | "findFeature"
  | "writeStory"
  | "writeTestCases"
  | "readStory"
  | "refinementPrompt"
  | "collectRefinement"
  | "collectArchitectAnswers"
  | "runArchitect"
  | "startArchitectRunView"
  | "startDashboard"
  | "readRunRoleLog"
  | "readRunRolePatch"
> &
  Partial<CommandRuntimeDependencies> & {
    runRecoveryRepository?: RunRecoveryRepository;
  };

export async function collectRefinement(): Promise<{
  story: string;
  testCases: string;
}> {
  const problem = await textarea({ label: "Problem / user story" });
  const user = await textarea({ label: "User or audience" });
  const outcome = await textarea({
    label: "Desired outcome and acceptance criteria",
  });
  const constraints = await textarea({
    label: "Constraints and non-goals (optional)",
  });
  const testCases = await textarea({
    label: "QA test cases and regression scenarios",
  });
  const guidelines = await textarea({
    label: "Implementation and design guidance (optional)",
  });
  return {
    story: formatRefinementBrief({
      problem,
      audience: user,
      outcome,
      constraints,
      guidelines,
    }),
    testCases,
  };
}

export async function collectArchitectAnswers(
  questions: string,
): Promise<string> {
  return textarea({
    label: `Architect questions:\n\n${questions}\n\nProvide the decisions or answers`,
  });
}

export function architectProgressMessage(transcript: string): string {
  const latestEvent = extractArchitectEvents(transcript).at(-1);
  if (latestEvent?.type === "patch") {
    return "Architect is applying the specification patch";
  }
  if (latestEvent?.type === "tool-call") {
    return `Architect is running: ${latestEvent.content.slice(0, 90)}`;
  }
  if (latestEvent?.type === "thought") {
    return `Architect reasoning: ${latestEvent.content.slice(0, 90)}`;
  }
  if (latestEvent?.type === "activity") {
    return `Architect: ${latestEvent.content.slice(0, 90)}`;
  }
  if (latestEvent?.type === "lifecycle") {
    return latestEvent.content;
  }
  return "Architect is refining the feature specification";
}

export async function runArchitect({
  projectRoot,
  runner,
  prompt,
  logFile,
  onProgress = () => {},
  onTranscript = () => {},
}: {
  projectRoot: string;
  runner: string;
  prompt: string;
  logFile: string;
  onProgress?: (message: string) => void;
  onTranscript?: (transcript: string) => void;
}): Promise<{ logFile: string }> {
  await mkdir(path.dirname(logFile), { recursive: true });
  const contextFile = path.join(path.dirname(logFile), "architect-context.md");
  const assignmentFile = path.join(
    path.dirname(logFile),
    "architect-assignment.json",
  );
  const responseFile = path.join(
    path.dirname(logFile),
    "architect-agent-response.json",
  );
  const questionsFile = path.join(path.dirname(logFile), "questions.md");
  await writeFile(
    contextFile,
    redactSecrets(`${prompt}\n\n${agentResponseContractPrompt()}\n`),
  );
  const assignment = createAgentAssignmentV1({
    assignmentId: `${path.basename(path.dirname(logFile))}:architect`,
    role: "architect",
    roleKind: "architect",
    objective:
      "Refine the approved feature packet using the referenced context. Modify only packet artifacts. Return completed with artifact claims, or needs_input with structured questions.",
    ownedPaths: ["specs"],
    contextReferences: [path.relative(projectRoot, contextFile)],
    acceptanceCriteria: [
      "Produce an implementation-ready feature packet grounded in repository evidence.",
      "Return structured clarification questions when a product decision is missing.",
    ],
    contracts: ["specs"],
    expectedCapabilities: ["repository-read", "workspace-write"],
  });
  const assignmentValidation = validateAgentAssignmentV1(assignment);
  if (!assignmentValidation.valid)
    throw new Error(
      `Invalid architect assignment: ${assignmentValidation.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
    );
  await writeFile(assignmentFile, `${JSON.stringify(assignment, null, 2)}\n`);
  return new Promise((resolve, reject) => {
    const adapter = runnerAdapter(runner);
    if (!adapter) {
      reject(new Error(`Architect runner adapter is unavailable: ${runner}.`));
      return;
    }
    const baseArgs = adapter.buildArgs(assignmentFile);
    const args = configureFinalOutputCapture(
      runner,
      [baseArgs[0]!, "--sandbox", "workspace-write", ...baseArgs.slice(1)],
      responseFile,
    );
    const child: ChildProcess = spawn(adapter.command, args, {
      cwd: projectRoot,
      stdio: ["ignore", "pipe", "pipe"],
      env: agentProcessEnvironment(),
    });
    activeArchitectProcesses.set(logFile, child);
    let transcript = "";
    const capture = (chunk: Buffer | string) => {
      transcript += redactSecrets(String(chunk));
      void writeFile(logFile, transcript);
      onProgress(architectProgressMessage(transcript));
      onTranscript(transcript);
    };
    child.stdout?.on("data", capture);
    child.stderr?.on("data", capture);
    child.on("error", reject);
    child.on("close", async (code: number | null) => {
      activeArchitectProcesses.delete(logFile);
      await writeFile(
        logFile,
        `--- Architect pass ${new Date().toISOString()} ---\n\n${transcript}`,
      );
      const captured = await readFile(responseFile, "utf8").catch(() => "");
      if (captured) await writeFile(responseFile, redactSecrets(captured));
      const output = captureFinalResponse(
        runner,
        path.basename(path.dirname(logFile)),
        "architect",
        transcript,
        "",
        captured,
      );
      const structural = parseAgentResponseV1(output);
      const semantic =
        structural.valid && structural.value
          ? validateAgentResponseForAssignment(structural.value, {
              roleKind: "architect",
              ownedPaths: ["specs"],
            })
          : structural;
      if (
        code === 0 &&
        structural.valid &&
        semantic.valid &&
        structural.value?.status === "needs_input"
      ) {
        await writeFile(
          questionsFile,
          renderClarificationQuestions(structural.value),
        );
        resolve({ logFile });
      } else if (
        code === 0 &&
        structural.valid &&
        semantic.valid &&
        structural.value?.status === "completed"
      ) {
        resolve({ logFile });
      } else {
        const detail = transcript.trim().slice(-2_000);
        reject(
          new Error(
            `Architect run failed protocol completion with exit code ${code}: ${[...structural.issues, ...semantic.issues].map((item) => `${item.path}: ${item.message}`).join("; ") || `status ${structural.value?.status ?? "missing"}`}.${detail ? `\n\n${detail}` : ""}\n\nFull log: ${logFile}`,
          ),
        );
      }
    });
  });
}

export async function refineCommand(
  featureId: string,
  storyArgument: string | undefined,
  options: {
    project?: string;
    architect?: boolean;
    testCases?: string;
    interactive?: boolean;
    compact?: boolean;
  },
  dependencies: Partial<RefinementCommandDependencies>,
): Promise<RefinementResult> {
  const {
    output,
    progress,
    loadConfig,
    findFeature,
    writeStory,
    writeTestCases,
    readStory,
    refinementPrompt,
    collectRefinement: collectRef,
    collectArchitectAnswers: collectAnswers,
    runArchitect: runArch,
    startArchitectRunView,
    startDashboard,
    readRunRoleLog,
    readRunRolePatch,
  } = defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project);
  const config = await loadConfig(projectRoot);
  const feature = await findFeature({ projectRoot, config, featureId });
  const refinement =
    options.architect && !storyArgument
      ? {
          story: await readStory(feature),
          testCases: undefined as string | undefined,
          existing: true as const,
        }
      : storyArgument
        ? {
            story: storyArgument,
            testCases: (options.testCases as string) ?? "",
            existing: false as const,
          }
        : options.interactive === false
          ? undefined
          : await collectRef().then((r) => ({
              ...r,
              existing: false as const,
            }));
  if (!refinement?.story?.trim())
    throw new Error(
      "Provide a story argument or run interactively to answer refinement questions.",
    );
  const storyFile = refinement.existing
    ? undefined
    : await progress("Saving feature story", () =>
        writeStory(feature, refinement.story),
      );
  const testCasesFile = refinement.existing
    ? undefined
    : await progress("Saving QA test cases", () =>
        writeTestCases(feature, String(refinement.testCases ?? "")),
      );
  if (storyFile) output(`Saved story to ${storyFile}`);
  if (testCasesFile) output(`Saved QA test cases to ${testCasesFile}`);
  if (!options.architect) {
    output(
      "Draft saved. Run again with --architect to refine the spec and contracts.",
    );
    return { feature, storyFile, testCasesFile, architectRan: false };
  }
  const logFile = path.join(
    projectRoot,
    config.stateDir ?? ".conduit",
    "runs",
    `refine-${feature.id}-${Date.now()}`,
    "architect.log",
  );
  const architectRunner =
    config.roles.architect?.runner ?? defaultConfig.roles.architect.runner;
  const architectRun: Run = {
    id: path.basename(path.dirname(logFile)),
    featureId: feature.id,
    status: "running",
    createdAt: new Date().toISOString(),
    roles: [
      {
        name: "architect",
        runner: architectRunner,
        readOnly: false,
        owns: [],
        dependsOn: [],
        promptFile: "",
        prompt: "",
        command: "",
        args: [],
        skillSource: "",
        status: "running",
      },
    ],
  };
  await mkdir(path.dirname(logFile), { recursive: true });
  let snapshot =
    await dependencies.runRecoveryRepository?.saveSnapshot(architectRun);
  const useTui =
    !options.compact && process.stdin.isTTY && process.stdout.isTTY;
  let liveView:
    | Awaited<ReturnType<ApplicationDependencies["startArchitectRunView"]>>
    | undefined;
  let showDashboard = false;
  const openLiveView = async () => {
    if (!useTui || liveView) return;
    try {
      liveView = await startArchitectRunView({
        featureId: feature.id,
        onUserClose: () => {
          showDashboard = false;
          liveView = undefined;
        },
      });
      showDashboard = true;
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error);
      output(
        `Live dashboard unavailable (${message}); using compact progress instead.`,
      );
    }
  };
  const questionsFile = path.join(path.dirname(logFile), "questions.md");
  const clarificationsFile = path.join(feature.directory, "clarifications.md");
  const prompt = refinementPrompt(
    feature,
    refinement.story,
    undefined,
    questionsFile,
  );
  let architect: { logFile: string } | undefined;
  let pass = 0;
  try {
    while (pass < 4) {
      await rm(questionsFile, { force: true });
      await openLiveView();
      const execute = ({
        setText = (_: string): void => {},
      }: { setText?: (text: string) => void } = {}) =>
        runArch({
          projectRoot,
          runner: architectRunner,
          prompt,
          logFile,
          onProgress: setText,
          onTranscript: (transcript: string) => liveView?.update(transcript),
        });
      architect = liveView
        ? await execute({})
        : await progress(
            pass
              ? "Continuing refinement with your answers"
              : "Refining specification with the architect",
            execute,
          );
      const questions = (
        await readFile(questionsFile, "utf8").catch(() => "")
      ).trim();
      if (!questions) break;
      architectRun.status = "awaiting-input";
      architectRun.roles[0].status = "awaiting-input";
      snapshot = await dependencies.runRecoveryRepository?.saveSnapshot(
        architectRun,
        snapshot?.version,
      );
      liveView?.close();
      liveView = undefined;
      output(
        `Architect needs clarification. Questions saved to ${questionsFile}`,
      );
      if (options.interactive === false)
        throw new Error(
          `Architect questions require an interactive answer. Review ${questionsFile} and rerun without --no-interactive.`,
        );
      const answers = await collectAnswers(questions);
      if (!answers?.trim())
        throw new Error("Architect clarification was cancelled or empty.");
      await appendFile(
        clarificationsFile,
        `# Clarification pass ${pass + 1}\n\n## Questions\n\n${questions}\n\n## Decisions\n\n${answers.trim()}\n\n`,
      );
      await rm(questionsFile, { force: true });
      pass += 1;
    }
    if (pass === 4)
      throw new Error(
        "Architect requested clarification four times. Review clarifications.md and refine the story before retrying.",
      );
    architectRun.status = "completed";
    architectRun.roles[0].status = "completed";
    snapshot = await dependencies.runRecoveryRepository?.saveSnapshot(
      architectRun,
      snapshot?.version,
    );
    liveView?.close();
  } catch (error) {
    if (architectRun.status !== "awaiting-input") {
      architectRun.status = "failed";
      architectRun.roles[0].status = "failed";
      await dependencies.runRecoveryRepository?.saveSnapshot(
        architectRun,
        snapshot?.version,
      );
      await dependencies.runRecoveryRepository?.markInterrupted(
        architectRun.id,
        error instanceof Error ? error.message : String(error),
      );
    }
    throw error;
  } finally {
    liveView?.close();
  }
  output(
    `Architect refinement completed. Captured transcript: ${architect?.logFile}`,
  );
  output("Review spec.md and contracts before running workers.");
  if (showDashboard)
    await startDashboard({
      projectRoot,
      config,
      runs: [architectRun as unknown as Run],
      selectedRunId: architectRun.id,
      readRoleLog: readRunRoleLog,
      readRolePatch: readRunRolePatch,
    });
  return {
    feature,
    storyFile,
    testCasesFile,
    architectRan: true,
    logFile: architect?.logFile,
  };
}
