import type { UpdateProgressStage } from "../enums/update-progress-stage.js";

export interface UpdateProgressEvent {
  readonly stage: UpdateProgressStage;
  readonly message: string;
  readonly completedBytes?: number;
  readonly totalBytes?: number;
}
