import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { Theme } from "../theme.js";

interface RunEventRowProps {
  readonly event: RunnerEvent;
  readonly theme: Theme;
}

function eventDescription(event: RunnerEvent): string {
  const { payload } = event;
  switch (payload.kind) {
    case "lifecycle":
      return payload.message ?? `State: ${payload.state}`;
    case "activity":
      return payload.message;
    case "tool-call":
      return `Called ${payload.tool}${payload.args ? `(${payload.args.slice(0, 60)})` : ""}`;
    case "tool-output":
      return `${payload.tool} → ${payload.output.slice(0, 80)}${payload.truncated ? "…" : ""}`;
    case "file-change":
      return `Changed ${payload.path} (+${payload.additions} -${payload.deletions})`;
    case "patch":
      return `Patch: ${payload.fileCount} files`;
    case "error":
      return `[${payload.code}] ${payload.message}`;
    case "result":
      return `Exit ${payload.exitCode}: ${payload.summary}`;
  }
}

export function RunEventRow({ event, theme }: RunEventRowProps) {
  const description = eventDescription(event);
  const time = new Date(event.timestamp).toLocaleTimeString();
  return (
    <box flexDirection="row">
      <text content={`${time} `} fg={theme.text.muted} />
      <text content={`[${event.type}] `} fg={theme.action.primary} />
      <text content={description} fg={theme.text.default} />
    </box>
  );
}
