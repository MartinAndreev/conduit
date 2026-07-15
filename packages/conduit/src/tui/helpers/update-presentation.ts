import { UpdateStatus } from "@domains/updates/enums/update-status.js";
import type {
  AvailableUpdateStatus,
  UpdateStatusReadModel,
} from "@domains/updates/types/update-status-read-model.js";

export function updateStatusLabel(status: UpdateStatusReadModel): string {
  switch (status.status) {
    case UpdateStatus.Idle:
      return "";
    case UpdateStatus.Checking:
      return "checking";
    case UpdateStatus.Current:
      return "up to date";
    case UpdateStatus.Available:
      return `v${status.targetVersion} available`;
    case UpdateStatus.Unavailable:
      return "update status unavailable";
    case UpdateStatus.Updating:
      return status.progress.stage;
    case UpdateStatus.Succeeded:
      return "update ready";
    case UpdateStatus.Failed:
      return "update failed";
  }
}

export function updateConfirmationDetails(
  status: AvailableUpdateStatus,
): readonly string[] {
  return [
    `Current: v${status.currentVersion}`,
    `Target:  v${status.targetVersion}`,
    `Method:  ${status.installation?.label ?? "Manual update"}`,
  ];
}
