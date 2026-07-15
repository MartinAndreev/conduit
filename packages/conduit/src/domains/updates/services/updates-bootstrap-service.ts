import { conduitVersion } from "../../../version.js";
import { createCheckForUpdateHandler } from "../handlers/check-for-update-handler.js";
import { createStartUpdateHandler } from "../handlers/start-update-handler.js";
import type { ReleaseDiscovery } from "../interfaces/release-discovery.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import { GitHubReleaseDiscovery } from "../repositories/github-release-discovery.js";
import { DefaultInstallationDetector } from "../repositories/default-installation-detector.js";
import type { InstallationDetector } from "../interfaces/installation-detector.js";
import { StrategyUpdateInstaller } from "../repositories/strategy-update-installer.js";
import { InMemoryUpdateStatusRepository } from "../repositories/in-memory-update-status-repository.js";
import { createGetUpdateStatusHandler } from "../handlers/get-update-status-handler.js";
import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../../../system/bootstrap/interfaces/application-bootstrap.js";

export class UpdatesBootstrapService implements ApplicationBootstrapService {
  private readonly statusRepository: InMemoryUpdateStatusRepository;

  constructor(
    private readonly discovery: ReleaseDiscovery = new GitHubReleaseDiscovery(),
    private readonly installer: UpdateInstaller = new StrategyUpdateInstaller(),
    private readonly currentVersion: string = conduitVersion,
    private readonly installationDetector: InstallationDetector = new DefaultInstallationDetector(),
  ) {
    this.statusRepository = new InMemoryUpdateStatusRepository(currentVersion);
  }

  register(context: ApplicationBootstrapContext): void {
    context.queryBus.register(
      "checkForUpdate",
      createCheckForUpdateHandler(
        this.discovery,
        this.currentVersion,
        this.installationDetector,
        this.statusRepository,
      ) as QueryHandler,
    );
    context.queryBus.register(
      "getUpdateStatus",
      createGetUpdateStatusHandler(this.statusRepository) as QueryHandler,
    );
    context.commandBus.register(
      "startUpdate",
      createStartUpdateHandler(
        this.installer,
        this.currentVersion,
        this.statusRepository,
      ) as CommandHandler,
    );
  }
}
