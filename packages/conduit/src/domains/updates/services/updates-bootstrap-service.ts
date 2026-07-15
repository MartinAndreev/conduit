import { conduitVersion } from "../../../version.js";
import { createCheckForUpdateHandler } from "../handlers/check-for-update-handler.js";
import { createStartUpdateHandler } from "../handlers/start-update-handler.js";
import type { ReleaseDiscovery } from "../interfaces/release-discovery.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import { GitHubReleaseDiscovery } from "../repositories/github-release-discovery.js";
import { UnavailableUpdateInstaller } from "../repositories/unavailable-update-installer.js";
import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../../../system/bootstrap/interfaces/application-bootstrap.js";

export class UpdatesBootstrapService implements ApplicationBootstrapService {
  constructor(
    private readonly discovery: ReleaseDiscovery = new GitHubReleaseDiscovery(),
    private readonly installer: UpdateInstaller = new UnavailableUpdateInstaller(),
    private readonly currentVersion: string = conduitVersion,
  ) {}

  register(context: ApplicationBootstrapContext): void {
    context.queryBus.register(
      "checkForUpdate",
      createCheckForUpdateHandler(
        this.discovery,
        this.currentVersion,
      ) as QueryHandler,
    );
    context.commandBus.register(
      "startUpdate",
      createStartUpdateHandler(
        this.installer,
        this.currentVersion,
      ) as CommandHandler,
    );
  }
}
