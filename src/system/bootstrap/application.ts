import type { Config } from "../../domains/configuration/types/config.js";
import type { Feature } from "../../domains/features/types/feature.js";
import type { Run } from "../../domains/runs/types/run.js";
import { CommandBus } from "../bus/command-bus.js";
import type { Command, CommandHandler } from "../bus/command-bus.js";
import { QueryBus } from "../bus/query-bus.js";
import type { Query, QueryHandler } from "../bus/query-bus.js";

export interface Application {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
}

export interface BootstrapDependencies {
  loadConfig: (projectRoot: string) => Promise<Config>;
  initializeProject: (
    projectRoot: string,
    templateRoot: string,
    embeddedTemplates?: Record<string, string>,
  ) => Promise<{ createdConfig: boolean; configFile: string }>;
  createFeature: (params: {
    projectRoot: string;
    config: Config;
    title: string;
  }) => Promise<Feature>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  planRun: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
    roleNames: string[];
    builtinRoot: string;
    fetchSkills?: boolean;
  }) => Promise<{ run: Run; runDir: string }>;
  latestRuns: (projectRoot: string, config: Config) => Promise<Run[]>;
}

interface InitProjectPayload {
  projectRoot: string;
  templateRoot: string;
}

interface ProjectBootstrapState {
  initialized: boolean;
  configPath?: string;
}

export function createApplication(deps: BootstrapDependencies): Application {
  const commandBus = new CommandBus();
  const queryBus = new QueryBus();

  commandBus.register("initializeProject", (async (
    cmd: Command & InitProjectPayload,
  ) => {
    const result = await deps.initializeProject(
      cmd.projectRoot,
      cmd.templateRoot,
    );
    return { success: true, data: result };
  }) as CommandHandler);

  queryBus.register("projectBootstrapState", (async (
    q: Query & { projectRoot: string },
  ) => {
    try {
      await deps.loadConfig(q.projectRoot);
      return {
        success: true,
        data: {
          initialized: true,
          configPath: `${q.projectRoot}/conduit.yml`,
        },
      };
    } catch {
      return {
        success: true,
        data: { initialized: false } satisfies ProjectBootstrapState,
      };
    }
  }) as QueryHandler);

  queryBus.register("latestRuns", (async (
    q: Query & { projectRoot: string },
  ) => {
    const config = await deps.loadConfig(q.projectRoot);
    const runs = await deps.latestRuns(q.projectRoot, config);
    return { success: true, data: runs };
  }) as QueryHandler);

  return { commandBus, queryBus };
}
