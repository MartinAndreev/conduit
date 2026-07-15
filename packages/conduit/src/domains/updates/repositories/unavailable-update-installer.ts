import { UpdatePlatformError } from "../errors/update-errors.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import type { UpdateProgressEvent } from "../types/update-progress.js";
import type { UpdateRequest } from "../types/update-request.js";

export class UnavailableUpdateInstaller implements UpdateInstaller {
  async install(
    _request: UpdateRequest,
    _onProgress: (event: UpdateProgressEvent) => void,
  ): Promise<void> {
    throw new UpdatePlatformError(
      "INSTALLER_NOT_AVAILABLE",
      "Automatic updating is not available for this installation.",
    );
  }
}
