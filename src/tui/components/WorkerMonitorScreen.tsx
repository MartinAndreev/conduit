import { useTheme } from "./ThemeProvider.js";
import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { ChangedFile } from "@domains/runs/types/review.js";
import type {
  RolePresentation,
  WorkerMonitorFocus,
} from "@tui/types/worker-monitor.js";
import { formatEventDescription } from "@tui/helpers/event-presentation.js";
import { AgentActivity } from "./AgentActivity.js";
import { SplitDiff } from "./SplitDiff.js";
import { WorktreeChanges } from "./WorktreeChanges.js";

interface WorkerMonitorScreenProps {
  readonly runId: string;
  readonly events: readonly RunnerEvent[];
  readonly roles: readonly RolePresentation[];
  readonly selectedRoleIndex: number;
  readonly expandedEventIndex: number | null;
  readonly transcriptExpanded: boolean;
  readonly scrollOffset: number;
  readonly selectedFileDiff: string | undefined;
  readonly fileDiffExpanded: boolean;
  readonly changedFiles: readonly ChangedFile[];
  readonly totalAdditions: number;
  readonly totalDeletions: number;
  readonly selectedFileIndex: number;
  readonly loading: boolean;
  readonly error: string | null;
  readonly cancelled: boolean;
  readonly focusMode: WorkerMonitorFocus;
}

export function WorkerMonitorScreen(props: WorkerMonitorScreenProps) {
  const theme = useTheme();
  if (props.loading)
    return (
      <box width="100%" height="100%" padding={1}>
        <text content="Loading worker monitor…" fg={theme.text.muted} />
      </box>
    );
  if (props.error)
    return (
      <box width="100%" height="100%" padding={1}>
        <text content={`Error: ${props.error}`} fg={theme.status.error} />
      </box>
    );

  const role = props.roles[props.selectedRoleIndex];
  const visibleEvents = props.events.slice(
    props.scrollOffset,
    props.scrollOffset + 8,
  );
  const hasUnavailable = props.roles.some((item) => item.isUnavailable);
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <text
        content={`Conduit · Worker Monitor · ${props.runId}`}
        fg={theme.text.strong}
      />
      <text
        content="Tab focus · ↑/↓ navigate · Enter select/toggle · j/k scroll activity · Ctrl+C cancel · q exit"
        fg={theme.text.muted}
      />
      {props.cancelled && (
        <text
          content="Cancellation requested; waiting for the runner lifecycle event."
          fg={theme.action.attention}
        />
      )}
      {hasUnavailable && (
        <text
          content="A runner is unavailable. Install it or check PATH, then retry the run."
          fg={theme.action.attention}
        />
      )}
      <box flexDirection="row" width="100%" flexGrow={1} marginTop={1}>
        <box
          width="30%"
          flexDirection="column"
          backgroundColor={theme.surface.raised}
          padding={1}
        >
          <text
            content={`${props.focusMode === "roles" ? "▸ " : ""}Roles`}
            fg={theme.text.strong}
          />
          {props.roles.map((item, index) => (
            <text
              key={item.roleId}
              content={`${index === props.selectedRoleIndex ? "›" : " "} ${item.roleId} · ${item.state}`}
              fg={
                index === props.selectedRoleIndex
                  ? theme.action.primary
                  : theme.text.default
              }
            />
          ))}
        </box>
        <box
          width="70%"
          flexDirection="column"
          padding={1}
          backgroundColor={theme.surface.raised}
        >
          {role ? (
            <>
              <AgentActivity
                role={role.roleId}
                runner=""
                message={role.message}
                state={role.state}
                mascotRole={
                  role.roleId === "architect" ? "architect" : undefined
                }
              />
              <text
                content={`${props.focusMode === "activity" ? "▸ " : ""}Activity (${props.events.length} events)`}
                fg={theme.text.strong}
              />
              {visibleEvents.map((event, index) => {
                const eventIndex = props.scrollOffset + index;
                const expanded =
                  props.transcriptExpanded &&
                  props.expandedEventIndex === eventIndex;
                return (
                  <box
                    key={`${event.timestamp}-${eventIndex}`}
                    flexDirection="column"
                  >
                    <text
                      content={`${eventIndex === props.expandedEventIndex ? "›" : " "}${formatEventDescription(event)}`}
                      fg={
                        event.type === "error"
                          ? theme.status.error
                          : theme.text.default
                      }
                    />
                    {expanded && (
                      <text
                        content={JSON.stringify(event.payload)}
                        fg={theme.text.muted}
                      />
                    )}
                  </box>
                );
              })}
              <box flexDirection="column" marginTop={1}>
                <text
                  content={`${props.focusMode === "files" ? "▸ " : ""}Changed files`}
                  fg={theme.text.strong}
                />
                <WorktreeChanges
                  changedFiles={props.changedFiles}
                  selectedFileIndex={props.selectedFileIndex}
                  totalAdditions={props.totalAdditions}
                  totalDeletions={props.totalDeletions}
                  theme={theme}
                />
                {props.fileDiffExpanded && (
                  <SplitDiff diff={props.selectedFileDiff} height={10} />
                )}
              </box>
            </>
          ) : (
            <text
              content="Select a role to view its activity"
              fg={theme.text.muted}
            />
          )}
        </box>
      </box>
    </box>
  );
}
