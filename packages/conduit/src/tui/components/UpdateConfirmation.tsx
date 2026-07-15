import type { AvailableUpdateStatus } from "@domains/updates/types/update-status-read-model.js";
import type { Theme } from "@tui/theme.js";
import { updateConfirmationDetails } from "@tui/helpers/update-presentation.js";

interface UpdateConfirmationProps {
  readonly status: AvailableUpdateStatus;
  readonly selectedAction: 0 | 1;
  readonly theme: Theme;
}

export function UpdateConfirmation({
  status,
  selectedAction,
  theme,
}: UpdateConfirmationProps) {
  const [current, target, method] = updateConfirmationDetails(status);
  return (
    <box width="70%" height="100%" flexDirection="column" padding={1}>
      <box
        flexDirection="column"
        borderStyle="single"
        borderColor={theme.action.attention}
        padding={1}
      >
        <text content="Update Conduit?" fg={theme.text.strong} />
        <text content="" />
        <text content={current} fg={theme.text.default} />
        <text content={target} fg={theme.action.attention} />
        <text content={method} fg={theme.text.default} />
        <text content="" />
        <text
          content={`${selectedAction === 0 ? ">" : " "} Cancel`}
          fg={selectedAction === 0 ? theme.action.primary : theme.text.default}
        />
        <text
          content={`${selectedAction === 1 ? ">" : " "} Update`}
          fg={selectedAction === 1 ? theme.action.primary : theme.text.default}
        />
      </box>
    </box>
  );
}
