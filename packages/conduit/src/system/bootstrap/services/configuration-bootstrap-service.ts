import { createResolveSettingsHandler } from "../../../domains/configuration/handlers/resolve-settings-handler.js";
import { createDeleteCredentialHandler } from "../../../domains/credentials/handlers/delete-credential-handler.js";
import { createGetCredentialHandler } from "../../../domains/credentials/handlers/get-credential-handler.js";
import { createListCredentialKeysHandler } from "../../../domains/credentials/handlers/list-credential-keys-handler.js";
import { createSetCredentialHandler } from "../../../domains/credentials/handlers/set-credential-handler.js";
import type { CommandHandler } from "../../bus/command-bus.js";
import type { QueryHandler } from "../../bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../interfaces/application-bootstrap.js";

export class ConfigurationBootstrapService implements ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void {
    const { commandBus, queryBus, dependencies } = context;
    commandBus.register(
      "setCredential",
      createSetCredentialHandler(
        dependencies.credentialStore,
      ) as CommandHandler,
    );
    commandBus.register(
      "deleteCredential",
      createDeleteCredentialHandler(
        dependencies.credentialStore,
      ) as CommandHandler,
    );
    queryBus.register(
      "resolveSettings",
      createResolveSettingsHandler(
        dependencies.configurationRepository,
      ) as QueryHandler,
    );
    queryBus.register(
      "getCredential",
      createGetCredentialHandler(dependencies.credentialStore) as QueryHandler,
    );
    queryBus.register(
      "listCredentialKeys",
      createListCredentialKeysHandler(
        dependencies.credentialStore,
      ) as QueryHandler,
    );
  }
}
