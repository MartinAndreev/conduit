import { createListPortraitsHandler } from "../../../domains/roles/handlers/list-portraits-handler.js";
import type { QueryHandler } from "../../bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../interfaces/application-bootstrap.js";

export class RolesBootstrapService implements ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void {
    const { queryBus, dependencies, projectRoot } = context;
    queryBus.register(
      "listPortraits",
      createListPortraitsHandler(dependencies.portraitRegistry) as QueryHandler,
    );
    if (projectRoot)
      queryBus.register("listRunRoles", (async () => {
        const config = await dependencies.loadConfig(projectRoot);
        return {
          success: true,
          data: Object.entries(config.roles).map(([name, role]) => ({
            name,
            runner: role.runner,
            description: role.description,
          })),
        };
      }) as QueryHandler);
  }
}
