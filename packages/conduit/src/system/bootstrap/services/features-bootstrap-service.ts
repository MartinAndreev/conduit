import { createCreateFeatureHandler } from "../../../domains/features/handlers/create-feature-handler.js";
import { createGetFeatureContentHandler } from "../../../domains/features/handlers/get-feature-content-handler.js";
import { createGetFeatureHandler } from "../../../domains/features/handlers/get-feature-handler.js";
import { createListFeaturesHandler } from "../../../domains/features/handlers/list-features-handler.js";
import { createUpdateFeatureMetadataHandler } from "../../../domains/features/handlers/update-feature-metadata-handler.js";
import type { CommandHandler } from "../../bus/command-bus.js";
import type { QueryHandler } from "../../bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../interfaces/application-bootstrap.js";

export class FeaturesBootstrapService implements ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void {
    const { commandBus, queryBus, dependencies, projectRoot } = context;
    commandBus.register(
      "updateFeatureMetadata",
      createUpdateFeatureMetadataHandler(
        dependencies.featureProvider,
      ) as CommandHandler,
    );
    if (projectRoot)
      commandBus.register(
        "createFeature",
        createCreateFeatureHandler({
          projectRoot,
          loadConfig: dependencies.loadConfig,
          createFeature: dependencies.createFeature,
        }) as CommandHandler,
      );

    queryBus.register(
      "listFeatures",
      createListFeaturesHandler(dependencies.featureProvider) as QueryHandler,
    );
    queryBus.register(
      "getFeature",
      createGetFeatureHandler(dependencies.featureProvider) as QueryHandler,
    );
    queryBus.register(
      "getFeatureContent",
      createGetFeatureContentHandler(
        dependencies.featureProvider,
      ) as QueryHandler,
    );
  }
}
