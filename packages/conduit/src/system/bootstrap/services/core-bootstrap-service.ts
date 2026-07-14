import type { Command, CommandHandler } from "../../bus/command-bus.js";
import type { Query, QueryHandler } from "../../bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../interfaces/application-bootstrap.js";

type InitProjectPayload = {
  projectRoot: string;
  templateRoot: string;
};

type ProjectBootstrapState = {
  initialized: boolean;
  configPath?: string;
};

export class CoreBootstrapService implements ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void {
    const { commandBus, queryBus, dependencies } = context;
    commandBus.register("initializeProject", (async (
      command: Command & InitProjectPayload,
    ) => {
      const result = await dependencies.initializeProject(
        command.projectRoot,
        command.templateRoot,
      );
      return { success: true, data: result };
    }) as CommandHandler);

    queryBus.register("projectBootstrapState", (async (
      query: Query & { projectRoot: string },
    ) => {
      try {
        await dependencies.loadConfig(query.projectRoot);
        return {
          success: true,
          data: {
            initialized: true,
            configPath: `${query.projectRoot}/conduit.yml`,
          },
        };
      } catch {
        return {
          success: true,
          data: { initialized: false } satisfies ProjectBootstrapState,
        };
      }
    }) as QueryHandler);
  }
}
