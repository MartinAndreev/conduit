import { UpdateStatus } from "@domains/updates/enums/update-status.js";
import type { UpdateStatusReadModel } from "@domains/updates/types/update-status-read-model.js";
import type { Theme } from "@tui/theme.js";
import { updateStatusLabel } from "@tui/helpers/update-presentation.js";

interface HomeVersionStatusProps {
  readonly status: UpdateStatusReadModel;
  readonly theme: Theme;
}

export function HomeVersionStatus({ status, theme }: HomeVersionStatusProps) {
  const detail = updateStatusLabel(status);
  const color =
    status.status === UpdateStatus.Available
      ? theme.action.attention
      : status.status === UpdateStatus.Current ||
          status.status === UpdateStatus.Succeeded
        ? theme.action.primary
        : status.status === UpdateStatus.Failed
          ? theme.status.error
          : theme.text.muted;
  return (
    <box width="100%" height={1} flexDirection="row" paddingLeft={1}>
      <text
        content={`Conduit v${status.currentVersion}${detail ? ` · ${detail}` : ""}`}
        fg={color}
      />
    </box>
  );
}
