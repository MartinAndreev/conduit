import { CommandBus } from "../bus/command-bus.js";
import { QueryBus } from "../bus/query-bus.js";
import type {
  Application,
  ApplicationBootstrapService,
  BootstrapDependencies,
} from "./interfaces/application-bootstrap.js";
import { createBootstrapComposition } from "./services/create-bootstrap-composition.js";
import { createDefaultBootstrapServices } from "./services/default-bootstrap-services.js";

export type {
  Application,
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
  BootstrapDependencies,
  BootstrapRepositories,
} from "./interfaces/application-bootstrap.js";

export function createApplication(
  dependencies: BootstrapDependencies,
  bootstrapServices: readonly ApplicationBootstrapService[] = createDefaultBootstrapServices(),
): Application {
  const commandBus = new CommandBus();
  const queryBus = new QueryBus();
  const composition = createBootstrapComposition(
    commandBus,
    queryBus,
    dependencies,
  );

  for (const service of bootstrapServices)
    service.register(composition.context);

  return {
    commandBus,
    queryBus,
    async close() {
      await composition.lifecycle.shutdown();
    },
  };
}
