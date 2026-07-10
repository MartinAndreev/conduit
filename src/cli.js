import path from "node:path";
import { fileURLToPath } from "node:url";
import { Command } from "commander";
import { initializeProject, loadConfig } from "./config.js";
import {
  createFeature,
  findFeature,
  writeStory,
  writeTestCases,
  readStory,
  refinementPrompt,
} from "./features.js";
import { planRun, executeRun, latestRuns } from "./runs.js";
import { resolveSkill } from "./skills.js";
import { initCommand } from "./commands/init.js";
import { featureCommand } from "./commands/feature.js";
import { rolesCommand, resolveRoleCommand } from "./commands/roles.js";
import { runCommand } from "./commands/run.js";
import { statusCommand } from "./commands/status.js";
import {
  refineCommand,
  collectRefinement,
  collectArchitectAnswers,
  runArchitect,
} from "./commands/refine.js";
import { startDashboard } from "./tui/dashboard.js";
import { startArchitectRunView } from "./tui/architect-run.js";
import { startWorkerRunView } from "./tui/worker-run.js";
import { conduitVersion } from "./version.js";
import { conduitBanner, shouldShowBanner } from "./banner.js";
import { roleTemplates } from "./role-templates.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const dependencies = {
  initializeProject,
  loadConfig,
  createFeature,
  findFeature,
  writeStory,
  writeTestCases,
  readStory,
  refinementPrompt,
  collectRefinement,
  collectArchitectAnswers,
  runArchitect,
  startDashboard,
  startArchitectRunView,
  startWorkerRunView,
  planRun,
  executeRun,
  latestRuns,
  resolveSkill,
  builtinRoot: path.join(root, "skills", "roles"),
  templatesRoot: path.join(root, "skills"),
  roleTemplates,
};

function withProject(command) {
  return command.option(
    "-p, --project <path>",
    "project directory",
    process.cwd(),
  );
}

export function createProgram(injectedDependencies = {}) {
  const handlers = { ...dependencies, ...injectedDependencies };
  const program = new Command()
    .name("conduit")
    .description("Spec-driven orchestration for coding agents")
    .version(conduitVersion)
    .showHelpAfterError();

  program
    .command("init [path]")
    .description("bootstrap Conduit in an existing Git repository")
    .option("--dry-run", "show the target without changing files")
    .action((target, options) => initCommand(target, options, handlers));
  program
    .command("version")
    .description("print the Conduit version")
    .action(() => console.log(conduitVersion));
  withProject(
    program
      .command("feature <title>")
      .description("create a committed specification and contract packet"),
  ).action((title, options) => featureCommand(title, options, handlers));
  withProject(
    program
      .command("refine <feature-id> [story]")
      .description(
        "capture a story and optionally have Codex refine the specification",
      ),
  )
    .option(
      "--architect",
      "run Codex to update the feature specification and contracts",
    )
    .option(
      "--compact",
      "use a compact spinner instead of the live architect dashboard",
    )
    .option("--test-cases <text>", "QA cases for non-interactive refinement")
    .option(
      "--no-interactive",
      "require a story argument instead of asking questions",
    )
    .action((featureId, story, options) =>
      refineCommand(featureId, story, options, handlers),
    );
  withProject(
    program.command("roles").description("list configured specialist roles"),
  ).action((options) => rolesCommand(options, handlers));
  const role = program
    .command("role")
    .description("inspect and validate role skills");
  withProject(
    role
      .command("resolve <name>")
      .description("validate a role skill source")
      .option(
        "--fetch-skills",
        "fetch a verified remote skill when it is not cached",
      ),
  ).action((name, options) => resolveRoleCommand(name, options, handlers));
  withProject(
    program
      .command("run <feature-id>")
      .description("plan or execute isolated role runs")
      .requiredOption(
        "-r, --roles <roles>",
        "comma-separated roles, for example frontend,backend",
      )
      .option("--dry-run", "plan commands without launching agents")
      .option(
        "--compact",
        "use a compact spinner instead of the live worker dashboard",
      )
      .option("--fetch-skills", "fetch verified remote skills when needed"),
  ).action((featureId, options) => runCommand(featureId, options, handlers));
  withProject(
    program
      .command("status")
      .description("show recent local run state")
      .option(
        "--tui",
        "open an interactive dashboard with collapsed agent output",
      ),
  ).action((options) => statusCommand(options, handlers));
  return program;
}

export async function main(args) {
  if (shouldShowBanner(args)) process.stdout.write(`${conduitBanner}\n`);
  await createProgram().parseAsync(args, { from: "user" });
}
