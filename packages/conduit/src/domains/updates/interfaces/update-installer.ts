import type { UpdateProgressEvent } from "../types/update-progress.js";
import type { UpdateRequest } from "../types/update-request.js";

export interface UpdateInstaller {
  install(
    request: UpdateRequest,
    onProgress: (event: UpdateProgressEvent) => void,
  ): Promise<void>;
}
