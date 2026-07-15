import { InstallationKind } from "../enums/installation-kind.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import type { UpdateProgressEvent } from "../types/update-progress.js";
import type { UpdateRequest } from "../types/update-request.js";
import { GuidedUpdateInstaller } from "./guided-update-installer.js";
import { PackageUpdateInstaller } from "./package-update-installer.js";
import { VerifiedStandaloneInstaller } from "./verified-standalone-installer.js";

export class StrategyUpdateInstaller implements UpdateInstaller {
  constructor(
    private readonly standalone: UpdateInstaller = new VerifiedStandaloneInstaller(),
    private readonly packageManager: UpdateInstaller = new PackageUpdateInstaller(),
    private readonly guided: UpdateInstaller = new GuidedUpdateInstaller(),
  ) {}

  install(
    request: UpdateRequest,
    onProgress: (event: UpdateProgressEvent) => void,
  ): Promise<void> {
    if (request.installation.kind === InstallationKind.Standalone)
      return this.standalone.install(request, onProgress);
    if (request.installation.kind === InstallationKind.GlobalPackage)
      return this.packageManager.install(request, onProgress);
    return this.guided.install(request, onProgress);
  }
}
