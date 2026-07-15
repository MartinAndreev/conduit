export type AgentActivityState = "working" | "waiting" | "completed" | "failed";

export interface AgentActivityProps {
  /** Human-readable role currently performing work. */
  readonly role: string;
  /** Optional configured runner or model label. */
  readonly runner?: string;
  /** Short, changing description of the agent's current activity. */
  readonly message: string;
  readonly state?: AgentActivityState;
  /** Provided by the run controller when elapsed time should be displayed. */
  readonly elapsedSeconds?: number;
  /** Width of the indeterminate track, in terminal cells. */
  readonly trackWidth?: number;
  /** Optional native-pixel role preview positioned beside this dashboard row. */
  readonly mascotRole?: import("@tui/components/WorkflowMascotPreview.js").WorkflowRole;
}
