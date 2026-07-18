import { useTheme } from "./ThemeProvider.js";
import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { ChangedFile } from "@domains/runs/types/review.js";
import type { ResumeEligibility } from "@domains/runs/types/resume-eligibility.js";
import type {
  RolePresentation,
  WorkerMonitorFocus,
} from "@tui/types/worker-monitor.js";
import { AgentActivity } from "./AgentActivity.js";
import { AgentEventLog } from "./AgentEventLog.js";
import { SplitDiff } from "./SplitDiff.js";
import { WorktreeChanges } from "./WorktreeChanges.js";
import { WORKFLOW_ROLES, type WorkflowRole } from "./WorkflowMascotPreview.js";

function mascotForRole(roleId: string): WorkflowRole | undefined {
  return WORKFLOW_ROLES.includes(roleId as WorkflowRole)
    ? (roleId as WorkflowRole)
    : undefined;
}

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
  readonly canResume: boolean;
  readonly showRecovery: boolean;
  readonly resumeEligibility: ResumeEligibility | undefined;
  readonly resuming: boolean;
  readonly resumeError: string | null;
  readonly focusMode: WorkerMonitorFocus;
  readonly cancelOnExit?: boolean;
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
  const hasUnavailable = props.roles.some((item) => item.isUnavailable);
  const showFiles = props.focusMode === "files";
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <box width="100%" flexDirection="column" flexShrink={0}>
        <text
          content={`Conduit · ${props.cancelOnExit ? "Research preflight" : "Worker Monitor"} · ${props.runId}`}
          fg={theme.text.strong}
        />
        <text
          content="1 Roles  |  2 Logs  |  3 Files  |  Up/Down Navigate"
          fg={theme.text.muted}
        />
        <text
          content={
            props.cancelOnExit
              ? "Enter Open  |  Ctrl+C Cancel  |  Esc/q Exit"
              : `Enter Open  |  Ctrl+C Cancel  |  q Exit${props.canResume ? `  |  r ${props.resuming ? "Resuming" : "Resume"}` : ""}`
          }
          fg={theme.text.muted}
        />
        {props.showRecovery && props.resumeEligibility && (
          <text
            content={
              props.resumeEligibility.state === "resumable"
                ? `Recovery verified · preserve: ${props.resumeEligibility.preservedRoles.join(", ") || "none"} · retry: ${props.resumeEligibility.retryRoles.join(", ")}`
                : `Recovery unavailable · ${props.resumeEligibility.reason ?? "run identity could not be verified"}`
            }
            fg={
              props.resumeEligibility.state === "resumable"
                ? theme.action.primary
                : theme.action.attention
            }
          />
        )}
        {props.showRecovery && !props.resumeEligibility && (
          <text
            content="Recovery · checking eligibility…"
            fg={theme.text.muted}
          />
        )}
        {props.resumeError && (
          <text
            content={`Resume failed: ${props.resumeError}`}
            fg={theme.status.error}
          />
        )}
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
      </box>
      <box
        flexDirection="row"
        width="100%"
        flexGrow={1}
        flexShrink={1}
        minHeight={0}
        marginTop={1}
      >
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
                mascotRole={mascotForRole(role.roleId)}
              />
              <box flexDirection="row" marginTop={1}>
                <text
                  content={`${showFiles ? " " : "["}Logs (${props.events.length})${showFiles ? " " : "]"}`}
                  fg={showFiles ? theme.text.muted : theme.action.primary}
                />
                <text content="  " />
                <text
                  content={`${showFiles ? "[" : " "}Files (${props.changedFiles.length})${showFiles ? "]" : " "}`}
                  fg={showFiles ? theme.action.primary : theme.text.muted}
                />
              </box>
              {showFiles ? (
                <box flexDirection="column" flexGrow={1} minHeight={0}>
                  <WorktreeChanges
                    changedFiles={props.changedFiles}
                    selectedFileIndex={props.selectedFileIndex}
                    totalAdditions={props.totalAdditions}
                    totalDeletions={props.totalDeletions}
                    theme={theme}
                    maxVisibleFiles={props.fileDiffExpanded ? 4 : 18}
                  />
                  {props.fileDiffExpanded && (
                    <box flexDirection="column" flexGrow={1} minHeight={0}>
                      <text
                        content={`Diff preview · ${props.selectedFileDiff?.split("\n").length ?? 0} patch lines · ↑/↓ or PgUp/PgDn scroll · Home/End jump`}
                        fg={theme.text.muted}
                      />
                      <SplitDiff diff={props.selectedFileDiff} height="100%" />
                    </box>
                  )}
                </box>
              ) : (
                <AgentEventLog
                  events={props.events}
                  scrollOffset={props.scrollOffset}
                  expandedEventIndex={props.expandedEventIndex}
                  transcriptExpanded={props.transcriptExpanded}
                />
              )}
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
