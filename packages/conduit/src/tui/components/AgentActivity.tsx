import { useEffect, useState } from "react";
import { formatIndeterminateProgress } from "@helpers/formatting/indeterminate-progress.js";
import type {
  AgentActivityProps,
  AgentActivityState,
} from "@tui/types/agent-activity.js";
import { WorkflowMascotPreview } from "./WorkflowMascotPreview.js";
import { useTheme } from "./ThemeProvider.js";

const SPINNER_FRAMES = ["◜", "◠", "◝", "◞", "◡", "◟"] as const;

function formatElapsed(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return minutes > 0
    ? `${minutes}m ${remainingSeconds}s`
    : `${remainingSeconds}s`;
}

function indicatorFor(state: AgentActivityState, frame: number): string {
  if (state === "completed") return "✓";
  if (state === "failed") return "×";
  if (state === "waiting") return "○";
  return SPINNER_FRAMES[frame % SPINNER_FRAMES.length]!;
}

/** Compact OpenCode-style activity status for work without measurable progress. */
export function AgentActivity({
  role,
  runner,
  message,
  state = "working",
  elapsedSeconds,
  trackWidth = 18,
  mascotRole,
}: AgentActivityProps) {
  const theme = useTheme();
  const [frame, setFrame] = useState(0);
  const isActive = state === "working";

  useEffect(() => {
    if (!isActive) return;
    const timer = setInterval(() => setFrame((current) => current + 1), 120);
    return () => clearInterval(timer);
  }, [isActive]);

  const color =
    state === "failed"
      ? theme.status.error
      : state === "waiting"
        ? theme.text.muted
        : state === "completed"
          ? theme.action.primary
          : theme.action.attention;
  const elapsed =
    elapsedSeconds === undefined ? undefined : formatElapsed(elapsedSeconds);

  return (
    <box flexDirection="row" alignItems="flex-start">
      {mascotRole ? <WorkflowMascotPreview role={mascotRole} /> : null}
      {mascotRole ? <text content="  " /> : null}
      <box flexDirection="column" marginTop={mascotRole ? 1 : 0}>
        <box flexDirection="row">
          <text content={`${indicatorFor(state, frame)} `} fg={color} />
          <text content={role} fg={theme.text.strong} />
          {runner ? (
            <text content={`  ${runner}`} fg={theme.text.muted} />
          ) : null}
          <text content={`  ${message}`} fg={theme.text.default} />
          {elapsed ? (
            <text content={`  ${elapsed}`} fg={theme.text.muted} />
          ) : null}
        </box>
        {isActive ? (
          <box flexDirection="row">
            <text
              content={formatIndeterminateProgress(frame, trackWidth)}
              fg={color}
            />
            <text content="  working" fg={theme.text.muted} />
          </box>
        ) : null}
      </box>
    </box>
  );
}
