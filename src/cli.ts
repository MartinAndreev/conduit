import path from "node:path";
import { fileURLToPath } from "node:url";
import { Command } from "commander";
import {
  initializeProject,
  loadConfig,
} from "./domains/configuration/repositories/project-config.js";
import {
  createFeature,
  findFeature,
  writeStory,
  writeTestCases,
  readStory,
  refinementPrompt,
} from "./domains/features/repositories/feature-packet-repository.js";
import {
  planRun,
  executeRun,
  latestRuns,
} from "./domains/runs/repositories/run-orchestrator.js";
import {
  readRunRoleLog,
  readRunRolePatch,
} from "./domains/runs/repositories/run-artifacts-repository.js";
import { resolveSkill } from "./domains/roles/repositories/skill-resolver.js";
import { initCommand } from "./domains/configuration/handlers/init-command.js";
import { featureCommand } from "./domains/features/handlers/feature-command.js";
import {
  rolesCommand,
  resolveRoleCommand,
} from "./domains/roles/handlers/roles-command.js";
import { runCommand } from "./domains/runs/handlers/run-command.js";
import { statusCommand } from "./domains/runs/handlers/status-command.js";
import {
  refineCommand,
  collectRefinement,
  collectArchitectAnswers,
  runArchitect,
  cancelArchitectForFeature,
} from "./domains/refinement/handlers/refine-command.js";
import { startDashboard } from "./tui/dashboard.js";
import { startArchitectRunView } from "./tui/architect-run.js";
import { startWorkerRunView } from "./tui/worker-run.js";
import { startRefinement } from "./tui/refinement.js";
import { refineCommandReact } from "./domains/refinement/handlers/refine-command-react.js";
import { conduitVersion } from "./version.js";
import { conduitBanner, shouldShowBanner } from "./banner.js";
import { roleTemplates } from "./domains/roles/assets/role-templates.js";
import { createConfigurationRepository } from "./domains/configuration/repositories/configuration-repository.js";
import {
  CompositeCredentialStore,
  EncryptedFallbackStore,
} from "./domains/credentials/repositories/encrypted-fallback-store.js";
import { OSVaultStore } from "./domains/credentials/repositories/os-vault-store.js";
import { LocalSpecKitProvider } from "./domains/features/providers/local-spec-kit-provider.js";
import { createPortraitRegistry } from "./domains/roles/repositories/portrait-registry.js";
import { createApplication } from "./system/bootstrap/application.js";
import { isGitRepository } from "./system/cli/command-support.js";
import { defaultPrompt, confirmYesNo } from "./helpers/prompt.js";
import type { PromptFn } from "./helpers/prompt.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

async function buildDependencies() {
  const configurationRepository = createConfigurationRepository();
  const globalConfigDir = configurationRepository.getGlobalConfigDir();

  const osVault = new OSVaultStore();
  const encryptedFallback = new EncryptedFallbackStore(globalConfigDir);
  const credentialStore = new CompositeCredentialStore(
    osVault,
    encryptedFallback,
  );

  await credentialStore.initialize();

  const portraitRegistry = createPortraitRegistry();

  return {
    configurationRepository,
    credentialStore,
    portraitRegistry,
    createProvider: (specsDir: string) => new LocalSpecKitProvider(specsDir),
  };
}

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
  startRefinement,
  planRun,
  executeRun,
  latestRuns,
  readRunRoleLog,
  readRunRolePatch,
  resolveSkill,
  builtinRoot: path.join(root, "skills", "roles"),
  templatesRoot: path.join(root, "skills"),
  roleTemplates,
};

function withProject(command: Command): Command {
  return command.option(
    "-p, --project <path>",
    "project directory",
    process.cwd(),
  );
}

export function createProgram(
  injectedDependencies: Record<string, unknown> = {},
): Command {
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
    .action((target: string, options: Record<string, unknown>) =>
      initCommand(target, options, handlers),
    );
  program
    .command("version")
    .description("print the Conduit version")
    .action(() => console.log(conduitVersion));
  withProject(
    program
      .command("feature <title>")
      .description("create a committed specification and contract packet"),
  ).action((title: string, options: Record<string, unknown>) => {
    void featureCommand(title, options, handlers);
  });
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
    .action(
      async (
        featureId: string,
        story: string,
        options: Record<string, unknown>,
      ) => {
        const isInteractive = options.interactive !== false && !options.compact;
        if (isInteractive) {
          const projectRoot = (options.project as string) || process.cwd();
          const settingsResult = await buildDependencies();
          const resolvedSettings =
            await settingsResult.configurationRepository.resolveSettings(
              projectRoot,
            );
          const specsDir = path.resolve(
            projectRoot,
            resolvedSettings.effective.specsDir,
          );
          const featureProvider = settingsResult.createProvider(specsDir);

          const app = createApplication({
            loadConfig,
            initializeProject,
            createFeature,
            findFeature,
            planRun,
            latestRuns,
            configurationRepository: settingsResult.configurationRepository,
            credentialStore: settingsResult.credentialStore,
            featureProvider,
            portraitRegistry: settingsResult.portraitRegistry,
            projectRoot,
            refinementPrompt,
            runArchitect,
            cancelArchitect: cancelArchitectForFeature,
          });

          await refineCommandReact(
            {
              featureId,
              story,
              testCases: options.testCases as string,
              architect: options.architect as boolean,
              compact: options.compact as boolean,
              interactive: options.interactive as boolean,
            },
            {
              commandBus: app.commandBus,
              queryBus: app.queryBus,
              startRefinementScreen: handlers.startRefinement,
              output: console.log,
            },
          );
        } else {
          void refineCommand(featureId, story, options, handlers);
        }
      },
    );
  withProject(
    program.command("roles").description("list configured specialist roles"),
  ).action((options: Record<string, unknown>) => {
    void rolesCommand(options, handlers);
  });
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
  ).action((name: string, options: Record<string, unknown>) => {
    void resolveRoleCommand(name, options, handlers);
  });
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
  ).action((featureId: string, options: Record<string, unknown>) => {
    void runCommand(featureId, options, handlers);
  });
  withProject(
    program
      .command("status")
      .description("show recent local run state")
      .option(
        "--tui",
        "open an interactive dashboard with collapsed agent output",
      ),
  ).action((options: Record<string, unknown>) => {
    void statusCommand(options, handlers);
  });
  return program;
}

export async function handleBareConduit(
  projectRoot: string,
  deps?: {
    prompt?: PromptFn;
    output?: (message: string) => void;
    startHome?: (params: {
      commandBus: ReturnType<typeof createApplication>["commandBus"];
      queryBus: ReturnType<typeof createApplication>["queryBus"];
    }) => Promise<void>;
    setExitCode?: (code: number) => void;
  },
): Promise<void> {
  const prompt = deps?.prompt ?? defaultPrompt;
  const output = deps?.output ?? console.log;
  const setExitCode =
    deps?.setExitCode ?? ((code: number) => (process.exitCode = code));

  if (!isGitRepository(projectRoot)) {
    output(
      "Not a Git repository. Navigate to a Git repository or run: conduit init [path]",
    );
    setExitCode(1);
    return;
  }

  const configExists = await loadConfig(projectRoot).then(
    () => true,
    () => false,
  );

  if (!configExists) {
    const shouldInit = await confirmYesNo(
      prompt,
      "Conduit is not initialized in this project. Initialize now?",
    );
    if (!shouldInit) {
      setExitCode(1);
      return;
    }

    await initializeProject(
      projectRoot,
      path.join(root, "skills"),
      roleTemplates,
    );
    output(`Conduit initialized in ${projectRoot}`);
  }

  const settingsResult = await buildDependencies();
  const resolvedSettings =
    await settingsResult.configurationRepository.resolveSettings(projectRoot);
  const specsDir = path.resolve(
    projectRoot,
    resolvedSettings.effective.specsDir,
  );
  const featureProvider = settingsResult.createProvider(specsDir);

  const app = createApplication({
    loadConfig,
    initializeProject,
    createFeature,
    findFeature,
    planRun,
    latestRuns,
    configurationRepository: settingsResult.configurationRepository,
    credentialStore: settingsResult.credentialStore,
    featureProvider,
    portraitRegistry: settingsResult.portraitRegistry,
    refinementPrompt,
    runArchitect,
    cancelArchitect: cancelArchitectForFeature,
    projectRoot,
  });

  const homeFn = deps?.startHome ?? (await import("./tui/home.js")).startHome;
  await homeFn({
    commandBus: app.commandBus,
    queryBus: app.queryBus,
  });
}

export async function main(args: string[]): Promise<void> {
  if (shouldShowBanner(args)) process.stdout.write(`${conduitBanner}\n`);

  if (args.length === 0) {
    await handleBareConduit(process.cwd());
    return;
  }

  await createProgram().parseAsync(args, { from: "user" });
}
