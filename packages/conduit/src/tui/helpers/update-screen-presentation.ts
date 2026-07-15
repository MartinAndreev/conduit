import { InstallationKind } from "@domains/updates/enums/installation-kind.js";
import { UpdateStatus } from "@domains/updates/enums/update-status.js";
import type { UpdateStatusReadModel } from "@domains/updates/types/update-status-read-model.js";
import type { UpdateScreenKeyAction } from "@tui/types/update-screen.js";

export function isUpdateAnimating(
  status: UpdateStatusReadModel | undefined,
): boolean {
  return !status || status.status === UpdateStatus.Updating;
}

export function updateScreenKeyAction(
  status: UpdateStatusReadModel | undefined,
  key: string,
): UpdateScreenKeyAction {
  if (isUpdateAnimating(status)) return "none";
  if (key === "h" || key === "escape") return "home";
  if (key === "r" && status?.status === UpdateStatus.Failed && status.retryable)
    return "retry";
  if (key === "q" && status?.status === UpdateStatus.Succeeded) return "quit";
  return "none";
}

export function updateSuccessGuidance(
  status: UpdateStatusReadModel,
): readonly string[] {
  if (status.status !== UpdateStatus.Succeeded) return [];
  if (status.installation.kind === InstallationKind.Standalone)
    return [
      "The running process is still the old version.",
      "Exit and restart Conduit to use the verified update.",
    ];
  if (status.installation.kind === InstallationKind.GlobalPackage)
    return ["The next Conduit launch will use the updated package version."];
  return [
    status.installation.manualCommand ??
      status.installation.manualUrl ??
      "Use the official release page to update manually.",
  ];
}

export function updateScreenActions(
  status: UpdateStatusReadModel | undefined,
): string {
  if (!status || status.status === UpdateStatus.Updating)
    return "Update in progress";
  if (status.status === UpdateStatus.Failed)
    return status.retryable
      ? "[r] Retry  [h/Esc] Return Home"
      : "[h/Esc] Return Home";
  if (status.status === UpdateStatus.Succeeded)
    return "[h] Return Home  [q] Exit Conduit";
  return "[h/Esc] Return Home";
}
