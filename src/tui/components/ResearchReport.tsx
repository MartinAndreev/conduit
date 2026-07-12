import { useKeyboard } from "@opentui/react";
import { AgentActivity } from "@tui/components/AgentActivity.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import type { Theme } from "@tui/theme.js";

interface ResearchReportProps {
  readonly theme: Theme;
  readonly report: string | null;
  readonly onAccept: () => void;
  readonly onRerun: () => void;
  readonly onEdit: () => void;
  readonly onExit: () => void;
}

export function ResearchReport({
  theme,
  report,
  onAccept,
  onRerun,
  onEdit,
  onExit,
}: ResearchReportProps) {
  useKeyboard((event: { name: string }) => {
    if (event.name === "a") onAccept();
    if (event.name === "r") onRerun();
    if (event.name === "e") onEdit();
    if (event.name === "q" || event.name === "escape") onExit();
  });

  if (!report)
    return (
      <box
        width="100%"
        height="100%"
        flexDirection="column"
        alignItems="center"
        justifyContent="center"
        backgroundColor={theme.surface.base}
      >
        <AgentActivity
          role="researcher"
          runner="configured runner"
          message="Inspecting repository context"
          state="working"
          mascotRole="researcher"
        />
        <text
          content="The architect will wait for your review."
          fg={theme.text.muted}
        />
      </box>
    );

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <box height={3} flexDirection="column">
        <text content="Research preflight" fg={theme.action.primary} />
        <text
          content="Review this repository context before the architect starts."
          fg={theme.text.muted}
        />
      </box>
      <box flexGrow={1} marginTop={1} backgroundColor={theme.surface.raised}>
        <MarkdownDocument content={report} />
      </box>
      <text
        content="a: accept → start architect · r: rerun research · e: edit brief · q: exit"
        fg={theme.text.muted}
      />
    </box>
  );
}
