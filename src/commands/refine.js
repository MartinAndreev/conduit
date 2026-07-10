import { appendFile, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { spawn } from "node:child_process";
import { defaultDependencies, resolveProject } from "./shared.js";
import { textarea } from "../tui/textarea.js";

export async function collectRefinement() {
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
  return {
    story: `Problem: ${problem}\n\nUser: ${user}\n\nDesired outcome: ${outcome}${constraints ? `\n\nConstraints and non-goals: ${constraints}` : ""}`,
    testCases,
  };
}

export async function collectArchitectAnswers(questions) {
  return textarea({
    label: `Architect questions:\n\n${questions}\n\nProvide the decisions or answers`,
  });
}

export function architectProgressMessage(transcript) {
  if (/apply patch/i.test(transcript))
    return "Codex is applying the specification patch";
  const command = [...transcript.matchAll(/(?:^|\n)exec\n([^\n]+)/g)].at(
    -1,
  )?.[1];
  if (command) return `Codex is running: ${command.slice(0, 90)}`;
  if (/^analysis$/m.test(transcript))
    return "Codex is analyzing the project context";
  if (/^codex$/m.test(transcript))
    return "Codex is refining the feature specification";
  return "Codex is refining the feature specification";
}

export async function runArchitect({
  projectRoot,
  prompt,
  logFile,
  onProgress = () => {},
  onTranscript = () => {},
}) {
  await mkdir(path.dirname(logFile), { recursive: true });
  return new Promise((resolve, reject) => {
    const child = spawn(
      "codex",
      ["exec", "--sandbox", "workspace-write", prompt],
      {
        cwd: projectRoot,
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let transcript = "";
    const capture = (chunk) => {
      transcript += chunk;
      onProgress(architectProgressMessage(transcript));
      onTranscript(transcript);
    };
    child.stdout.on("data", capture);
    child.stderr.on("data", capture);
    child.on("error", reject);
    child.on("close", async (code) => {
      await appendFile(
        logFile,
        `\n\n--- Architect pass ${new Date().toISOString()} ---\n\n${transcript}`,
      );
      if (code === 0) resolve({ logFile });
      else
        reject(
          new Error(
            `Codex architect run failed with exit code ${code}. Full log: ${logFile}`,
          ),
        );
    });
  });
}

export async function refineCommand(
  featureId,
  storyArgument,
  options,
  dependencies,
) {
  const {
    output,
    progress,
    loadConfig,
    findFeature,
    writeStory,
    writeTestCases,
    readStory,
    refinementPrompt,
    collectRefinement,
    collectArchitectAnswers,
    runArchitect,
    startArchitectRunView,
    startDashboard,
  } = defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project);
  const config = await loadConfig(projectRoot);
  const feature = await findFeature({ projectRoot, config, featureId });
  const refinement =
    options.architect && !storyArgument
      ? {
          story: await readStory(feature),
          testCases: undefined,
          existing: true,
        }
      : storyArgument
        ? { story: storyArgument, testCases: options.testCases ?? "" }
        : options.interactive === false
          ? undefined
          : await collectRefinement();
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
        writeTestCases(feature, refinement.testCases),
      );
  if (storyFile) output(`Saved story to ${storyFile}`);
  if (testCasesFile) output(`Saved QA test cases to ${testCasesFile}`);
  if (!options.architect) {
    output(
      "Draft saved. Run again with --architect to have Codex refine the spec and contracts.",
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
  const runFile = path.join(path.dirname(logFile), "run.json");
  const architectRun = {
    id: path.basename(path.dirname(logFile)),
    featureId: feature.id,
    status: "running",
    createdAt: new Date().toISOString(),
    roles: [{ name: "architect", runner: "codex", status: "running" }],
  };
  await mkdir(path.dirname(runFile), { recursive: true });
  await writeFile(runFile, JSON.stringify(architectRun, null, 2));
  const useTui =
    !options.compact && process.stdin.isTTY && process.stdout.isTTY;
  let liveView;
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
    } catch (error) {
      output(
        `Live dashboard unavailable (${error.message}); using compact progress instead.`,
      );
    }
  };
  const questionsFile = path.join(feature.directory, "questions.md");
  const clarificationsFile = path.join(feature.directory, "clarifications.md");
  const prompt = refinementPrompt(feature, refinement.story);
  let architect;
  let pass = 0;
  try {
    while (pass < 4) {
      await rm(questionsFile, { force: true });
      await openLiveView();
      const execute = ({ setText = () => {} } = {}) =>
        runArchitect({
          projectRoot,
          prompt,
          logFile,
          onProgress: setText,
          onTranscript: (transcript) => liveView?.update(transcript),
        });
      architect = liveView
        ? await execute()
        : await progress(
            pass
              ? "Continuing refinement with your answers"
              : "Refining specification with Codex",
            execute,
          );
      const questions = (
        await readFile(questionsFile, "utf8").catch(() => "")
      ).trim();
      if (!questions) break;
      architectRun.status = "awaiting-input";
      architectRun.roles[0].status = "awaiting-input";
      await writeFile(runFile, JSON.stringify(architectRun, null, 2));
      liveView?.close();
      liveView = undefined;
      output(
        `Architect needs clarification. Questions saved to ${questionsFile}`,
      );
      if (options.interactive === false)
        throw new Error(
          `Architect questions require an interactive answer. Review ${questionsFile} and rerun without --no-interactive.`,
        );
      const answers = await collectArchitectAnswers(questions);
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
    await writeFile(runFile, JSON.stringify(architectRun, null, 2));
    liveView?.close();
  } finally {
    liveView?.close();
  }
  output(
    `Codex refinement completed. Captured transcript: ${architect.logFile}`,
  );
  output("Review spec.md and contracts before running workers.");
  if (showDashboard)
    await startDashboard({
      projectRoot,
      config,
      runs: [architectRun],
      selectedRunId: architectRun.id,
    });
  return {
    feature,
    storyFile,
    testCasesFile,
    architectRan: true,
    logFile: architect.logFile,
  };
}
