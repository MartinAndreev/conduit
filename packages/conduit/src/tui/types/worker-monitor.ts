import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { ChangedFile } from "@domains/runs/types/review.js";
import type { Run } from "@domains/runs/types/run.js";
import type { ResumeEligibility } from "@domains/runs/types/resume-eligibility.js";

export interface RolePresentation {
  readonly roleId: string;
  readonly state: "working" | "completed" | "failed" | "waiting";
  readonly message: string;
  readonly eventCount: number;
  readonly isUnavailable: boolean;
}

export type WorkerMonitorFocus = "roles" | "files" | "activity";

export interface WorkerMonitorViewModel {
  readonly roles: readonly RolePresentation[];
  readonly selectedRoleIndex: number;
  readonly events: readonly RunnerEvent[];
  readonly diff: string | undefined;
  readonly selectedFileDiff: string | undefined;
  readonly changedFiles: readonly ChangedFile[];
  readonly selectedFileIndex: number;
  readonly totalAdditions: number;
  readonly totalDeletions: number;
  readonly loading: boolean;
  readonly error: string | null;
  readonly expandedEventIndex: number | null;
  readonly scrollOffset: number;
  readonly cancelled: boolean;
  readonly canResume: boolean;
  readonly showRecovery: boolean;
  readonly resumeEligibility: ResumeEligibility | undefined;
  readonly resuming: boolean;
  readonly resumeError: string | null;
  readonly focusMode: WorkerMonitorFocus;
  readonly transcriptExpanded: boolean;
  readonly fileDiffExpanded: boolean;
  selectRole(index: number): void;
  selectFile(index: number): void;
  toggleExpand(index: number): void;
  toggleFocus(): void;
  toggleTranscript(): void;
  cancel(): void;
}

export interface WorkerMonitorState {
  readonly events: readonly RunnerEvent[];
  readonly roles: readonly RolePresentation[];
  readonly run: Run | undefined;
  readonly resumeEligibility: ResumeEligibility | undefined;
  readonly selectedRoleIndex: number;
  readonly diff: string | undefined;
  readonly changedFiles: readonly ChangedFile[];
  readonly selectedFileIndex: number;
  readonly totalAdditions: number;
  readonly totalDeletions: number;
  readonly loading: boolean;
  readonly error: string | null;
  readonly expandedEventIndex: number | null;
  readonly scrollOffset: number;
  readonly cancelled: boolean;
  readonly resuming: boolean;
  readonly resumeError: string | null;
  readonly focusMode: WorkerMonitorFocus;
  readonly transcriptExpanded: boolean;
  readonly fileDiffExpanded: boolean;
}

export type WorkerMonitorAction =
  | { type: "eventsLoaded"; events: RunnerEvent[]; roles: RolePresentation[] }
  | { type: "runLoaded"; run: Run }
  | {
      type: "diffLoaded";
      diff: string | undefined;
      changedFiles: readonly ChangedFile[];
      totalAdditions: number;
      totalDeletions: number;
    }
  | { type: "loadError"; message: string }
  | { type: "selectRole"; index: number }
  | { type: "selectFile"; index: number }
  | { type: "toggleExpand"; index: number }
  | { type: "toggleFocus" }
  | { type: "setFocus"; focus: WorkerMonitorFocus }
  | { type: "toggleTranscript" }
  | { type: "toggleFileDiff" }
  | { type: "scroll"; direction: "up" | "down" }
  | { type: "cancel" }
  | { type: "resumeEligibilityLoaded"; eligibility: ResumeEligibility }
  | { type: "resumeStarted" }
  | { type: "resumeFinished" }
  | { type: "resumeFailed"; message: string };
