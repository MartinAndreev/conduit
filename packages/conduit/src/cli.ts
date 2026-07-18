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
} from "./domains/features/repositories/feature-packet-repository.js";
import { localSpecKitRefinementPrompt } from "./domains/features/providers/local-spec-kit-refinement-prompt.js";
import {
  planRun,
  executeRun,
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
import { createResumeRunHandler } from "./domains/runs/handlers/resume-run-handler.js";
import { createStartFeatureRunHandler } from "./domains/runs/handlers/start-feature-run-handler.js";
import { createGetRunResumeEligibilityHandler } from "./domains/runs/handlers/get-run-resume-eligibility-handler.js";
import { createGetWorkspaceContinuityHandler } from "./domains/runs/handlers/get-workspace-continuity-handler.js";
import type {
  StartFeatureRunCommand,
  StartFeatureRunResult,
} from "./domains/runs/interfaces/commands/start-feature-run.js";
import type {
  ResumeRunCommand,
  ResumeRunResult,
} from "./domains/runs/interfaces/commands/resume-run.js";
import type {
  GetRunResumeEligibilityQuery,
  GetRunResumeEligibilityReadModel,
} from "./domains/runs/interfaces/queries/get-run-resume-eligibility.js";
import type {
  GetWorkspaceContinuityQuery,
  GetWorkspaceContinuityReadModel,
} from "./domains/runs/interfaces/queries/get-workspace-continuity.js";
import { CommandBus } from "./system/bus/command-bus.js";
import { QueryBus } from "./system/bus/query-bus.js";
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
import type { ApplicationBootstrapService } from "./system/bootstrap/application.js";
import { createDefaultBootstrapServices } from "./system/bootstrap/services/default-bootstrap-services.js";
import { UpdatesBootstrapService } from "./domains/updates/services/updates-bootstrap-service.js";
import {
  GlobalDatabaseFactory,
  ProjectDatabaseFactory,
} from "./system/storage/factories/database-factories.js";
import { TursoGlobalProfileRepository } from "./domains/configuration/repositories/turso-global-profile-repository.js";
import { TursoGlobalConfigurationMetadataRepository } from "./domains/configuration/repositories/turso-global-configuration-metadata-repository.js";
import { DefaultStartupMigrationRunner } from "./system/storage/migrations/startup-migration-runner.js";
import { LegacyFileImporter } from "./system/storage/import/legacy-file-importer.js";
import { runMigrationScreen } from "./tui/migration.js";
import { isGitRepository } from "./system/cli/command-support.js";
import { defaultPrompt, confirmYesNo } from "./helpers/prompt.js";
import type { PromptFn } from "./helpers/prompt.js";
import { verifyStorageRuntime } from "./system/storage/diagnostics/storage-doctor.js";
import { TursoRunRecoveryRepository } from "./domains/runs/repositories/turso-run-recovery-repository.js";
import { TursoRunEventRepository } from "./domains/runs/repositories/turso-run-event-repository.js";
import { TursoRuntimeEventRepository } from "./domains/runs/repositories/turso-runtime-event-repository.js";
import { TursoConduitResultRecordRepository } from "./domains/runs/repositories/turso-conduit-result-record-repository.js";
import { TursoRoleWorkspaceRepository } from "./domains/runs/repositories/turso-role-workspace-repository.js";
import { createRunProcessRegistry } from "./domains/runs/repositories/run-process-registry.js";
import type { DatabaseConnection } from "./system/storage/interfaces/database.js";
import type { Config } from "./domains/configuration/types/config.js";
import { DefaultDatabaseLifecycle } from "./system/storage/repositories/database-lifecycle.js";
import { runAgentResponseMcpServer } from "./system/communication/services/agent-response-mcp-server.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

async function migrateProjectStorage(projectRoot: string): Promise<void> {
  await runMigrationScreen(async (onProgress) => {
    const config = await loadConfig(projectRoot);
    const stateDirectory = path.resolve(projectRoot, config.stateDir);
    const specsDirectory = path.resolve(projectRoot, config.specsDir);
    const importer = new LegacyFileImporter(
      projectRoot,
      stateDirectory,
      specsDirectory,
    );
    const runner = new DefaultStartupMigrationRunner(
      projectRoot,
      config.stateDir,
      importer,
    );
    await runner.run(onProgress);
  });
}

function createRunRecoveryDispatchers(input: {
  projectRoot: string;
  recoveryRepository: TursoRunRecoveryRepository;
  runEventRepository: TursoRunEventRepository;
  runtimeEventRepository: TursoRuntimeEventRepository;
  resultRecordRepository: TursoConduitResultRecordRepository;
  roleWorkspaceRepository: TursoRoleWorkspaceRepository;
  processRegistry: ReturnType<typeof createRunProcessRegistry>;
}) {
  const commandBus = new CommandBus();
  const queryBus = new QueryBus();
  commandBus.register(
    "resumeRun",
    createResumeRunHandler(input.recoveryRepository, {
      projectRoot: input.projectRoot,
      executeRun,
      eventRepository: input.runEventRepository,
      runtimeEventRepository: input.runtimeEventRepository,
      resultRepository: input.resultRecordRepository,
      processRegistry: input.processRegistry,
      roleWorkspaceRepository: input.roleWorkspaceRepository,
    }),
  );
  queryBus.register(
    "getWorkspaceContinuity",
    createGetWorkspaceContinuityHandler(
      input.projectRoot,
      input.recoveryRepository,
      input.roleWorkspaceRepository,
      input.resultRecordRepository,
    ),
  );
  queryBus.register(
    "getRunResumeEligibility",
    createGetRunResumeEligibilityHandler(
      input.recoveryRepository,
      input.resultRecordRepository,
      input.roleWorkspaceRepository,
    ),
  );
  commandBus.register(
    "startFeatureRun",
    createStartFeatureRunHandler({
      projectRoot: input.projectRoot,
      builtinRoot: path.join(root, "skills", "roles"),
      loadConfig,
      planRun,
      executeRun,
      recoveryRepository: input.recoveryRepository,
      roleWorkspaceRepository: input.roleWorkspaceRepository,
      eventRepository: input.runEventRepository,
      resultRepository: input.resultRecordRepository,
      runtimeEventRepository: input.runtimeEventRepository,
      processRegistry: input.processRegistry,
      getContinuity: async (featureId, roleNames) => {
        const result = await queryBus.execute<
          GetWorkspaceContinuityQuery,
          GetWorkspaceContinuityReadModel
        >({ type: "getWorkspaceContinuity", featureId, roleNames });
        if (!result.success) throw new Error(result.error.message);
        return result.data;
      },
      resumeRun: async (runId) =>
        commandBus.dispatch<ResumeRunCommand, ResumeRunResult>({
          type: "resumeRun",
          runId,
        }),
    }),
  );
  return {
    startFeatureRun: async (command: StartFeatureRunCommand) => {
      const result = await commandBus.dispatch<
        StartFeatureRunCommand,
        StartFeatureRunResult
      >(command);
      if (!result.success) throw new Error(result.error.message);
      return result.data;
    },
    resumeRun: async (runId: string) => {
      const result = await commandBus.dispatch<
        ResumeRunCommand,
        ResumeRunResult
      >({ type: "resumeRun", runId });
      if (!result.success) throw new Error(result.error.message);
      const resumed = await input.recoveryRepository.loadSnapshot(runId);
      if (!resumed) throw new Error(`Run ${runId} disappeared after resume.`);
      return resumed.run;
    },
    getWorkspaceContinuity: async (
      featureId: string,
      roleNames: readonly string[],
    ) => {
      const result = await queryBus.execute<
        GetWorkspaceContinuityQuery,
        GetWorkspaceContinuityReadModel
      >({ type: "getWorkspaceContinuity", featureId, roleNames });
      if (!result.success) throw new Error(result.error.message);
      return result.data;
    },
    getResumeEligibility: async (runId: string) => {
      const result = await queryBus.execute<
        GetRunResumeEligibilityQuery,
        GetRunResumeEligibilityReadModel
      >({
        type: "getRunResumeEligibility",
        projectRoot: input.projectRoot,
        runId,
      });
      if (!result.success) throw new Error(result.error.message);
      return result.data;
    },
  };
}

async function withProjectStorage<T>(
  projectRoot: string,
  work: (connection: DatabaseConnection, config: Config) => Promise<T>,
): Promise<T> {
  await migrateProjectStorage(projectRoot);
  const config = await loadConfig(projectRoot);
  const factory = new ProjectDatabaseFactory(
    projectRoot,
    undefined,
    config.stateDir,
  );
  const connection = await factory.open();
  const lifecycle = new DefaultDatabaseLifecycle();
  lifecycle.registerConnection(connection);
  try {
    return await work(connection, config);
  } finally {
    await lifecycle.shutdown();
  }
}

async function buildDependencies(
  projectRoot?: string,
  startupMigration: (root: string) => Promise<void> = migrateProjectStorage,
  environment: NodeJS.ProcessEnv = process.env,
) {
  if (projectRoot) await startupMigration(projectRoot);
  const globalDatabaseFactory = new GlobalDatabaseFactory(environment);
  const globalProfiles = new TursoGlobalProfileRepository(
    globalDatabaseFactory,
  );
  const globalMetadata = new TursoGlobalConfigurationMetadataRepository(
    globalDatabaseFactory,
  );
  const configurationRepository = createConfigurationRepository(globalProfiles);
  const globalConfigDir = configurationRepository.getGlobalConfigDir();

  const osVault = new OSVaultStore();
  const encryptedFallback = new EncryptedFallbackStore(globalConfigDir);
  const credentialStore = new CompositeCredentialStore(
    osVault,
    encryptedFallback,
  );

  await credentialStore.initialize();
  await globalMetadata.set("credentialProtection", {
    mode: credentialStore.isUsingFallback()
      ? "obfuscation-at-rest"
      : "os-bound-vault",
  });

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
  refinementPrompt: localSpecKitRefinementPrompt,
  collectRefinement,
  collectArchitectAnswers,
  runArchitect,
  startDashboard,
  startArchitectRunView,
  startWorkerRunView,
  startRefinement,
  planRun,
  executeRun,
  latestRuns: async () => [],
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
      .command("storage-doctor")
      .description("verify local database migrations and native runtime"),
  ).action(async (options: Record<string, unknown>) => {
    const projectRoot = (options.project as string) || process.cwd();
    await migrateProjectStorage(projectRoot);
    const config = await loadConfig(projectRoot);
    console.log(
      JSON.stringify(
        await verifyStorageRuntime(projectRoot, config.stateDir),
        null,
        2,
      ),
    );
  });
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
        "capture a story and optionally have the configured architect refine the specification",
      ),
  )
    .option(
      "--architect",
      "run the configured architect to update the feature specification and contracts",
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
          const settingsResult = await buildDependencies(projectRoot);
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
            executeRun,
            latestRuns: async () => [],
            configurationRepository: settingsResult.configurationRepository,
            credentialStore: settingsResult.credentialStore,
            featureProvider,
            portraitRegistry: settingsResult.portraitRegistry,
            projectRoot,
            stateDirectory: resolvedSettings.effective.stateDir,
            refinementPrompt: localSpecKitRefinementPrompt,
            runArchitect,
            cancelArchitect: cancelArchitectForFeature,
            builtinRoleRoot: handlers.builtinRoot as string,
            resolveRoleGuidance: handlers.resolveSkill as typeof resolveSkill,
          });

          try {
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
          } finally {
            await app.close();
          }
        } else {
          const projectRoot = (options.project as string) || process.cwd();
          await withProjectStorage(projectRoot, async (connection) => {
            await refineCommand(featureId, story, options, {
              ...handlers,
              runRecoveryRepository: new TursoRunRecoveryRepository(connection),
            });
          });
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
      .option("--fetch-skills", "fetch verified remote skills when needed")
      .option("--continue", "continue the compatible retained run")
      .option("--start-new", "start anew using the selected role slots")
      .option(
        "--confirm-discard-retained",
        "confirm destructive removal of retained worktree checkouts",
      ),
  ).action(async (featureId: string, options: Record<string, unknown>) => {
    const projectRoot = (options.project as string) || process.cwd();
    await withProjectStorage(projectRoot, async (connection) => {
      const recoveryRepository = new TursoRunRecoveryRepository(connection);
      const runEventRepository = new TursoRunEventRepository(connection);
      const runtimeEventRepository = new TursoRuntimeEventRepository(
        connection,
      );
      const resultRecordRepository = new TursoConduitResultRecordRepository(
        connection,
      );
      const processRegistry = createRunProcessRegistry();
      const roleWorkspaceRepository = new TursoRoleWorkspaceRepository(
        connection,
      );
      const recoveryDispatchers = createRunRecoveryDispatchers({
        projectRoot,
        recoveryRepository,
        runEventRepository,
        runtimeEventRepository,
        resultRecordRepository,
        roleWorkspaceRepository,
        processRegistry,
      });
      await runCommand(featureId, options, {
        ...handlers,
        runRecoveryRepository: recoveryRepository,
        runEventRepository,
        runtimeEventRepository,
        resultRecordRepository,
        roleWorkspaceRepository,
        runProcessRegistry: processRegistry,
        startFeatureRun: recoveryDispatchers.startFeatureRun,
      });
    });
  });
  withProject(
    program
      .command("status")
      .description("show recent local run state")
      .option(
        "--tui",
        "open an interactive dashboard with collapsed agent output",
      ),
  ).action(async (options: Record<string, unknown>) => {
    const projectRoot = (options.project as string) || process.cwd();
    await withProjectStorage(projectRoot, async (connection) => {
      const recoveryRepository = new TursoRunRecoveryRepository(connection);
      const runEventRepository = new TursoRunEventRepository(connection);
      const runtimeEventRepository = new TursoRuntimeEventRepository(
        connection,
      );
      const resultRecordRepository = new TursoConduitResultRecordRepository(
        connection,
      );
      const roleWorkspaceRepository = new TursoRoleWorkspaceRepository(
        connection,
      );
      const processRegistry = createRunProcessRegistry();
      const recoveryDispatchers = createRunRecoveryDispatchers({
        projectRoot,
        recoveryRepository,
        runEventRepository,
        runtimeEventRepository,
        resultRecordRepository,
        roleWorkspaceRepository,
        processRegistry,
      });
      await statusCommand(options, {
        ...handlers,
        latestRuns: async () =>
          (await recoveryRepository.listSnapshots(20)).map(({ run }) => run),
        resumeRun: recoveryDispatchers.resumeRun,
        getResumeEligibility: recoveryDispatchers.getResumeEligibility,
      });
    });
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
      updateChecksEnabled: boolean;
    }) => Promise<void>;
    setExitCode?: (code: number) => void;
    startupMigration?: (projectRoot: string) => Promise<void>;
    environment?: NodeJS.ProcessEnv;
    updatesBootstrapService?: ApplicationBootstrapService;
    checkForUpdates?: boolean;
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

  const settingsResult = await buildDependencies(
    projectRoot,
    deps?.startupMigration,
    deps?.environment,
  );
  const resolvedSettings =
    await settingsResult.configurationRepository.resolveSettings(projectRoot);
  const specsDir = path.resolve(
    projectRoot,
    resolvedSettings.effective.specsDir,
  );
  const featureProvider = settingsResult.createProvider(specsDir);

  const app = createApplication(
    {
      loadConfig,
      initializeProject,
      createFeature,
      findFeature,
      planRun,
      executeRun,
      latestRuns: async () => [],
      configurationRepository: settingsResult.configurationRepository,
      credentialStore: settingsResult.credentialStore,
      featureProvider,
      portraitRegistry: settingsResult.portraitRegistry,
      refinementPrompt: localSpecKitRefinementPrompt,
      runArchitect,
      cancelArchitect: cancelArchitectForFeature,
      builtinRoleRoot: path.join(root, "skills", "roles"),
      projectRoot,
      stateDirectory: resolvedSettings.effective.stateDir,
    },
    createDefaultBootstrapServices(
      deps?.updatesBootstrapService ?? new UpdatesBootstrapService(),
    ),
  );

  const homeFn = deps?.startHome ?? (await import("./tui/home.js")).startHome;
  try {
    if (deps?.checkForUpdates !== false)
      void app.queryBus.execute({ type: "checkForUpdate" });
    await homeFn({
      commandBus: app.commandBus,
      queryBus: app.queryBus,
      projectRoot,
      updateChecksEnabled: deps?.checkForUpdates !== false,
    });
  } finally {
    await app.close();
  }
}

export async function main(args: string[]): Promise<void> {
  if (args[0] === "__agent-response-mcp") {
    await runAgentResponseMcpServer();
    return;
  }
  if (shouldShowBanner(args)) process.stdout.write(`${conduitBanner}\n`);

  if (args.length === 0) {
    const interactive = Boolean(
      process.stdin.isTTY && process.stdout.isTTY && !process.env.CI,
    );
    await handleBareConduit(process.cwd(), { checkForUpdates: interactive });
    return;
  }

  await createProgram().parseAsync(args, { from: "user" });
}
