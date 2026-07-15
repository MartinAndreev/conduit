import { UpdateProgressStage } from "../enums/update-progress-stage.js";
import type { UpdateInstaller } from "../interfaces/update-installer.js";
import type { UpdateProgressEvent } from "../types/update-progress.js";
import type { UpdateRequest } from "../types/update-request.js";

export class GuidedUpdateInstaller implements UpdateInstaller {
  async install(
    request: UpdateRequest,
    onProgress: (event: UpdateProgressEvent) => void,
  ): Promise<void> {
    onProgress({
      stage: UpdateProgressStage.Preparing,
      message:
        request.installation.reason ?? "Preparing manual update guidance.",
    });
    onProgress({
      stage: UpdateProgressStage.Complete,
      message: "Manual update guidance is ready.",
    });
  }
}
